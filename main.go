package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

//go:embed web/index.html
var indexHTML string

//go:embed web/assets/*
var assetsFS embed.FS

// --- CONFIGURATION ---
const (
	DefaultPort   = ":8080"
	DefaultModel  = "gemini-2.0-flash"
	WorkDir       = "."
	HistoryPath   = ".history"
	TTLMinutes    = 120
	MaxFileBytes  = 256 * 1024 // 256KB cap per file
	MaxTotalChars = 4000000    // ~1M token safety cap
)

// --- GLOBAL STATE ---
var (
	ctx    = context.Background()
	client *genai.Client

	sessions = make(map[string][]*genai.Content)
	mu       sync.Mutex

	totalCost   float64
	cacheName   string
	cacheModel  string
	projectRoot string // Absolute path to the directory being served/cached
	serverHome  string // Absolute path to the directory where main.go lives
	serverPort  string
	debugMode   bool
	logFile     *os.File
	logMu       sync.Mutex
)

var modelCosts = map[string]struct{ In, Out float64 }{
	"gemini-1.5-flash":                    {0.075, 0.30},
	"gemini-1.5-flash-8b":                 {0.0375, 0.15},
	"gemini-1.5-pro":                      {1.25, 5.00},
	"gemini-2.0-flash":                    {0.10, 0.40},
	"gemini-2.0-flash-exp":                {0.00, 0.00},
	"gemini-2.0-flash-lite-preview-02-05": {0.075, 0.30},
	"gemini-exp-1206":                     {0.00, 0.00},
	"gemini-2.0-pro-exp-02-05":            {0.00, 0.00},
}


type ChatRequest struct {
	SessionID  string `json:"session_id"`
	Model      string `json:"model"`
	Message    string `json:"message"`
	CacheID    string `json:"cache_id"`    // Optional override
	UseSearch  bool   `json:"use_search"`  // Enable Google Search grounding
	UseAgentic bool   `json:"use_agentic"` // Enable file tools (write_file, etc.)
}

type ChatResponse struct {
	Text           string      `json:"text"`
	Images         []ImageData `json:"images,omitempty"`
	ToolCalls      []string    `json:"tool_calls,omitempty"`
	PromptTokens   int         `json:"prompt_tokens"`
	ResponseTokens int         `json:"response_tokens"`
	TotalTokens    int         `json:"total_tokens"`
	Cost           float64     `json:"request_cost_brl"`
	TotalCost      float64     `json:"session_total_brl"`
}

type ImageData struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64 encoded
}

// TemplateData holds data for HTML template rendering
type TemplateData struct {
	CacheName  string
	CacheModel string
	ServerPort string
	MCPPath    string
}

// --- LOGGING ---

func logMsg(format string, args ...any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	msg := fmt.Sprintf("[%s] %s", timestamp, fmt.Sprintf(format, args...))
	fmt.Println(msg)

	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.WriteString(msg + "\n")
	}
}

func initLogging() {
	// Create logs directory
	logsDir := filepath.Join(serverHome, "logs")
	os.MkdirAll(logsDir, 0755)

	// Open daily log file
	dateStr := time.Now().Format("2006-01-02")
	logPath := filepath.Join(logsDir, fmt.Sprintf("server_%s.log", dateStr))
	var err error
	logFile, err = os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Warning: Could not open log file: %v", err)
	}
}

func writeDebugResponse(content string) {
	if !debugMode {
		return
	}
	debugPath := filepath.Join(serverHome, "debug_last_response.txt")
	os.WriteFile(debugPath, []byte(content), 0644)
}

func main() {
	port := flag.String("port", DefaultPort, "Port to run the server on")
	cachePath := flag.String("cache", "", "Path to build context cache from (enables caching mode)")
	modelName := flag.String("model", DefaultModel, "Gemini model to use")
	cacheIDFlag := flag.String("cache-id", "", "Existing Cache ID to use directly")
	listModelsCmd := flag.Bool("list-models", false, "List available models and exit")
	debugFlag := flag.Bool("debug", false, "Enable debug mode (saves responses to file)")
	flag.Parse()

	serverPort = *port
	debugMode = *debugFlag

	// Capture serverHome (where the executable/source is)
	wd, _ := os.Getwd()
	serverHome = wd

	// Initialize logging
	initLogging()

	// Determine project root and cache mode
	var err error
	if *cachePath != "" {
		// Cache mode: use specified path or current directory
		path := *cachePath
		if path == "." || path == "" {
			path = wd
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			log.Fatalf("Could not resolve absolute path: %v", err)
		}
		projectRoot = absPath
	} else {
		// Clean mode (default): use current working directory
		projectRoot = wd
	}

	logMsg("--- Antigravity Brain Server ---")
	logMsg("--- Mode: %s ---", func() string {
		if *cacheIDFlag != "" {
			return "EXPLICIT CACHE"
		} else if *cachePath != "" {
			return "CACHE BUILD"
		}
		return "CLEAN (Stateless)"
	}())
	logMsg("--- Project Root: %s ---", projectRoot)
	logMsg("--- Server Home: %s ---", serverHome)

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		// Try loading from .env file
		if data, err := os.ReadFile(".env"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "GEMINI_API_KEY=") {
					apiKey = strings.TrimPrefix(line, "GEMINI_API_KEY=")
					os.Setenv("GEMINI_API_KEY", apiKey)
					break
				}
			}
		}
	}
	if apiKey == "" {
		log.Fatal("FATAL: GEMINI_API_KEY is not set.")
	}

	client, err = genai.NewClient(ctx, &genai.ClientConfig{
		APIKey: apiKey,
	})
	if err != nil {
		log.Fatal(err)
	}

	if *listModelsCmd {
		ListModels(client)
		return
	}

	// Cache setup based on mode
	if *cacheIDFlag != "" {
		// Explicit cache ID provided
		cacheName = *cacheIDFlag
		cacheModel = *modelName
		logMsg("--- Using Explicit Cache ID: %s ---", cacheName)
	} else if *cachePath != "" {
		// Build new cache from path
		logMsg("--- Building Context Cache for: %s ---", projectRoot)
		cacheName = BuildAndGetCache(client, projectRoot, *modelName)
		if cacheName != "" {
			os.Setenv("GEMINI_CACHE", cacheName)
			logMsg("--- Exported Environment Variable: GEMINI_CACHE=%s ---", cacheName)
		}
	} else {
		// Clean mode - no cache
		cacheModel = *modelName
		logMsg("--- Running in Clean Mode (no cache) ---")
	}


	// 3. START SERVER
	// Core endpoints
	http.HandleFunc("/chat", handleChat)
	http.HandleFunc("/reset", handleReset)
	http.HandleFunc("/files", handleFiles)
	http.HandleFunc("/models", handleModels)
	http.HandleFunc("/status", handleStatus)

	// Official Gemini API compatibility (for IDE SDKs)
	http.HandleFunc("/v1beta/models/", handleOfficialAPI)

	// OpenAI API compatibility (for tools expecting OpenAI)
	http.HandleFunc("/v1/models", handleOpenAIModels)
	http.HandleFunc("/v1/chat/completions", handleOpenAIChat)

	// Static assets and root
	http.HandleFunc("/assets/", handleAssets)
	http.HandleFunc("/", handleRoot)

	fmt.Printf("--- Server Running on %s ---\n", serverPort)
	if cacheName != "" {
		fmt.Printf("--- Cache Active: %s ---\n", cacheName)
	}
	log.Fatal(http.ListenAndServe(serverPort, nil))
}

// --- CORE LOGIC ---

func BuildAndGetCache(client *genai.Client, path, model string) string {
	var contentBuilder strings.Builder

	// Ingest history relative to project root
	historyPath := filepath.Join(projectRoot, HistoryPath)
	if hist, err := os.ReadFile(historyPath); err == nil {
		contentBuilder.WriteString("\n=== PROJECT HISTORY LOG ===\n")
		contentBuilder.Write(hist)
	}

	fileCount := 0
	filepath.WalkDir(projectRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		nameLower := strings.ToLower(d.Name())
		isBackup := strings.Contains(nameLower, "backup") || strings.Contains(nameLower, "bkup")

		if d.IsDir() {
			skipDirs := map[string]bool{
				".git": true, "node_modules": true, "venv": true, ".venv": true,
				"dist": true, "build": true, ".next": true, ".DS_Store": true,
				"target": true, "out": true, "images": true, "img": true,
				"media": true, "photos": true, "videos": true,
			}
			if skipDirs[d.Name()] || isBackup {
				return filepath.SkipDir
			}
			return nil
		}

		if isBackup {
			return nil
		}

		ext := filepath.Ext(p)
		// Explicitly allowed text/code formats. Note: .py is moved to restricted if it was there (it wasn't).
		allowed := map[string]bool{".md": true, ".txt": true, ".go": true, ".js": true, ".ts": true, ".json": true, ".lua": true, ".css": true, ".html": true}

		if allowed[ext] {
			if contentBuilder.Len() > MaxTotalChars {
				return filepath.SkipAll
			}

			info, err := d.Info()
			if err == nil && info.Size() > MaxFileBytes {
				// Skip files that are too large (minified bundles, large data)
				return nil
			}

			if data, err := os.ReadFile(p); err == nil {
				// Simple binary detection: check first 1KB for null bytes
				isBinary := false
				checkSize := len(data)
				if checkSize > 1024 {
					checkSize = 1024
				}
				for i := 0; i < checkSize; i++ {
					if data[i] == 0 {
						isBinary = true
						break
					}
				}

				if !isBinary {
					contentBuilder.WriteString(fmt.Sprintf("\n\n--- FILE: %s ---\n", p))
					contentBuilder.Write(data)
					fileCount++
				}
			}
		}
		return nil
	})

	fmt.Printf("Compiled %d files. Checking size...\n", fileCount)

	if contentBuilder.Len() < 32768 {
		fmt.Printf("--- Content size (%d bytes) is below Google's 32k token threshold. Adding padding to enable caching... ---\n", contentBuilder.Len())
		// Pad with a neutral comment to reach the threshold
		padding := strings.Repeat("\n// CACHE_PADDING_TOKEN_REDUNDANCY_FOR_COST_SAVINGS_PROTOCOL\n", (33000-contentBuilder.Len())/60)
		contentBuilder.WriteString(padding)
	}

	fmt.Println("Uploading to Google Context Cache...")

	// Create the cached content using new SDK API
	cache, err := client.Caches.Create(ctx, "models/"+model, &genai.CreateCachedContentConfig{
		DisplayName: "Unified_Project_Brain",
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				{Text: "You are Antigravity Brain, a powerful project assistant. You have access to the project's history and source code via your context cache. Always identify as Antigravity Brain / Gemini."},
			},
			Role: "user",
		},
		Contents: []*genai.Content{
			{
				Parts: []*genai.Part{
					{Text: contentBuilder.String()},
				},
				Role: "user",
			},
		},
		TTL: time.Duration(TTLMinutes) * time.Minute,
	})
	if err != nil {
		log.Printf("Cache Creation Failed (likely model unsupported or size limit): %v", err)
		return ""
	}

	cacheModel = model
	return cache.Name
}

// --- HANDLERS ---

func handleOfficialAPI(w http.ResponseWriter, r *http.Request) {
	// Check if this is a streaming request
	if strings.Contains(r.URL.Path, ":streamGenerateContent") {
		handleStream(w, r)
		return
	}
	handleChat(w, r)
}

// --- STATUS ENDPOINT ---
func handleStatus(w http.ResponseWriter, r *http.Request) {
	mode := "CLEAN"
	if cacheName != "" {
		mode = "CACHED"
	}

	status := map[string]any{
		"mode":         mode,
		"cache_id":     cacheName,
		"cache_model":  cacheModel,
		"project_root": projectRoot,
		"server_port":  serverPort,
		"debug_mode":   debugMode,
		"total_cost":   totalCost,
		"sessions":     len(sessions),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// --- OPENAI COMPATIBILITY ---

type OpenAIChatRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Stream bool `json:"stream"`
}

type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func handleOpenAIModels(w http.ResponseWriter, r *http.Request) {
	// Return actual Gemini models (excluding experimental)
	// Users can select any model from this list in Continue.dev
	var modelList []map[string]any

	// Fetch real models from Gemini API
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			break
		}
		// Check if model supports generateContent
		supportsGenerate := false
		for _, action := range m.SupportedActions {
			if action == "generateContent" {
				supportsGenerate = true
				break
			}
		}

		if supportsGenerate {
			geminiID := strings.TrimPrefix(m.Name, "models/")
			
			// Skip banned experimental models
			if strings.Contains(geminiID, "image-generation") || 
			   strings.Contains(geminiID, "-exp") || 
			   strings.Contains(geminiID, "experimental") ||
			   strings.Contains(geminiID, "2.0-flash-exp") ||
			   strings.Contains(geminiID, "2.0-pro-exp") {
				continue
			}

			// Return actual Gemini model ID - Continue.dev will show these in dropdown
			modelList = append(modelList, map[string]any{
				"id":       geminiID,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "gemini-proxy",
			})
		}
	}

	// Fallback if no models found
	if len(modelList) == 0 {
		defaultModel := cacheModel
		if defaultModel == "" {
			defaultModel = DefaultModel
		}
		modelList = []map[string]any{
			{
				"id":       defaultModel,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": "gemini-proxy",
			},
		}
	}

	response := map[string]any{
		"object": "list",
		"data":   modelList,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleOpenAIChat(w http.ResponseWriter, r *http.Request) {
	var req OpenAIChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}

	// Extract last user message
	userMsg := ""
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			userMsg = msg.Content
		}
	}

	if req.Stream {
		handleOpenAIStream(w, r, userMsg, req.Model)
		return
	}

	// Use model directly if it's a valid Gemini model ID, otherwise use cached/default
	model := req.Model
	
	// Check if it's a Gemini model ID and not banned
	if strings.HasPrefix(model, "gemini-") {
		// Block experimental models
		if strings.Contains(model, "-exp") || 
		   strings.Contains(model, "experimental") ||
		   strings.Contains(model, "2.0-flash-exp") ||
		   strings.Contains(model, "2.0-pro-exp") {
			http.Error(w, "Experimental models are not allowed", 400)
			return
		}
		// Use the specified Gemini model
	} else {
		// Not a Gemini model ID (e.g., "gpt-4"), use cached model or default
		model = cacheModel
		if model == "" {
			model = DefaultModel
		}
	}

	logMsg(">>> OpenAI /v1/chat/completions | Model: %s | Agentic: true | Msg: %.50s...", model, userMsg)

	// Create chat request
	chatReq := ChatRequest{
		SessionID: "openai-compat",
		Model:     model,
		Message:   userMsg,
	}

	// Get history
	mu.Lock()
	history := sessions[chatReq.SessionID]
	mu.Unlock()

	config := &genai.GenerateContentConfig{
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
		},
	}

	// Enable agentic tools for OpenAI endpoint (always enabled)
	fileTools := []*genai.FunctionDeclaration{
		{
			Name:        "write_file",
			Description: "Write or create a file with the specified content",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path":    {Type: genai.TypeString, Description: "Relative path to the file"},
					"content": {Type: genai.TypeString, Description: "Content to write to the file"},
				},
				Required: []string{"path", "content"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in the current directory or subdirectory",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path": {Type: genai.TypeString, Description: "Relative path to list (use '.' for current)"},
				},
			},
		},
		{
			Name:        "read_file",
			Description: "Read the contents of a specific file",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path": {Type: genai.TypeString, Description: "Relative path to the file"},
				},
				Required: []string{"path"},
			},
		},
	}

	// Skip cache when tools are enabled (Gemini API limitation)
	// Tools are always enabled for OpenAI endpoint, so skip cache
	config.Tools = []*genai.Tool{
		{FunctionDeclarations: fileTools},
	}

	chat, err := client.Chats.Create(ctx, model, config, history)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Handle tool calls in a loop (similar to handleChat)
	var responseText string
	res, err := chat.SendMessage(ctx, genai.Part{Text: userMsg})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	for {
		funcCalls := res.FunctionCalls()
		if len(funcCalls) == 0 {
			responseText = res.Text()
			break
		}

		// Execute function calls
		var funcResponses []genai.Part
		for _, funcCall := range funcCalls {
			var funcResult map[string]any
			args := funcCall.Args

			if funcCall.Name == "list_files" {
				p, _ := args["path"].(string)
				funcResult = toolListFiles(p)
			} else if funcCall.Name == "read_file" {
				p, _ := args["path"].(string)
				funcResult = toolReadFile(p)
			} else if funcCall.Name == "write_file" {
				p, _ := args["path"].(string)
				c, _ := args["content"].(string)
				funcResult = toolWriteFile(p, c)
			} else {
				funcResult = map[string]any{"error": "unknown tool"}
			}

			funcResponses = append(funcResponses, genai.Part{
				FunctionResponse: &genai.FunctionResponse{
					Name:     funcCall.Name,
					Response: funcResult,
				},
			})
		}

		res, err = chat.SendMessage(ctx, funcResponses...)
		if err != nil {
			responseText = "Error after tool execution: " + err.Error()
			break
		}
	}

	writeDebugResponse(responseText)

	// Store history
	mu.Lock()
	sessions[chatReq.SessionID] = chat.History(false)
	mu.Unlock()

	// Build OpenAI response
	response := OpenAIChatResponse{
		ID:      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
	}
	response.Choices = []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}{
		{
			Index: 0,
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "assistant", Content: responseText},
			FinishReason: "stop",
		},
	}

	if res.UsageMetadata != nil {
		response.Usage.PromptTokens = int(res.UsageMetadata.PromptTokenCount)
		response.Usage.CompletionTokens = int(res.UsageMetadata.CandidatesTokenCount)
		response.Usage.TotalTokens = int(res.UsageMetadata.TotalTokenCount)
	}

	logMsg("<<< OpenAI | Tokens: %din/%dout | Resp: %.50s...", response.Usage.PromptTokens, response.Usage.CompletionTokens, responseText)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleOpenAIStream(w http.ResponseWriter, r *http.Request, userMsg, reqModel string) {
	// Use model directly if it's a valid Gemini model ID, otherwise use cached/default
	model := reqModel
	
	// Check if it's a Gemini model ID and not banned
	if strings.HasPrefix(model, "gemini-") {
		// Block experimental models
		if strings.Contains(model, "-exp") || 
		   strings.Contains(model, "experimental") ||
		   strings.Contains(model, "2.0-flash-exp") ||
		   strings.Contains(model, "2.0-pro-exp") {
			fmt.Fprintf(w, "data: {\"error\": \"Experimental models are not allowed\"}\n\n")
			return
		}
		// Use the specified Gemini model
	} else {
		// Not a Gemini model ID (e.g., "gpt-4"), use cached model or default
		model = cacheModel
		if model == "" {
			model = DefaultModel
		}
	}

	logMsg(">>> OpenAI Stream | Model: %s | Agentic: true | Msg: %.50s...", model, userMsg)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	config := &genai.GenerateContentConfig{
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
		},
	}

	// Enable agentic tools for OpenAI endpoint (always enabled)
	fileTools := []*genai.FunctionDeclaration{
		{
			Name:        "write_file",
			Description: "Write or create a file with the specified content",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path":    {Type: genai.TypeString, Description: "Relative path to the file"},
					"content": {Type: genai.TypeString, Description: "Content to write to the file"},
				},
				Required: []string{"path", "content"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files in the current directory or subdirectory",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path": {Type: genai.TypeString, Description: "Relative path to list (use '.' for current)"},
				},
			},
		},
		{
			Name:        "read_file",
			Description: "Read the contents of a specific file",
			Parameters: &genai.Schema{
				Type: genai.TypeObject,
				Properties: map[string]*genai.Schema{
					"path": {Type: genai.TypeString, Description: "Relative path to the file"},
				},
				Required: []string{"path"},
			},
		},
	}

	// Skip cache when tools are enabled (Gemini API limitation)
	config.Tools = []*genai.Tool{
		{FunctionDeclarations: fileTools},
	}

	mu.Lock()
	history := sessions["openai-stream"]
	mu.Unlock()

	chat, err := client.Chats.Create(ctx, model, config, history)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\": \"%s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	// For tool-enabled chats, use non-streaming to handle function calls properly
	// Then stream the final response
	fullResponse := ""
	currentMsg := userMsg

	for {
		// Use non-streaming to detect function calls
		res, err := chat.SendMessage(ctx, genai.Part{Text: currentMsg})
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\": \"%s\"}\n\n", err.Error())
			flusher.Flush()
			return
		}

		// Check for function calls
		funcCalls := res.FunctionCalls()
		if len(funcCalls) > 0 {
			// Send function call notification in OpenAI format
			for _, funcCall := range funcCalls {
				chunk := map[string]any{
					"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   model,
					"choices": []map[string]any{
						{
							"index": 0,
							"delta": map[string]any{
								"role": "assistant",
								"tool_calls": []map[string]any{
									{
										"id":   funcCall.Name + "-" + fmt.Sprintf("%d", time.Now().UnixNano()),
										"type": "function",
										"function": map[string]any{
											"name":      funcCall.Name,
											"arguments": funcCall.Args,
										},
									},
								},
							},
							"finish_reason": "tool_calls",
						},
					},
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}

			// Execute function calls
			var funcResponses []genai.Part
			for _, funcCall := range funcCalls {
				var funcResult map[string]any
				args := funcCall.Args

				if funcCall.Name == "list_files" {
					p, _ := args["path"].(string)
					funcResult = toolListFiles(p)
				} else if funcCall.Name == "read_file" {
					p, _ := args["path"].(string)
					funcResult = toolReadFile(p)
				} else if funcCall.Name == "write_file" {
					p, _ := args["path"].(string)
					c, _ := args["content"].(string)
					funcResult = toolWriteFile(p, c)
				} else {
					funcResult = map[string]any{"error": "unknown tool"}
				}

				funcResponses = append(funcResponses, genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						Name:     funcCall.Name,
						Response: funcResult,
					},
				})
			}

			// Continue with function responses
			currentMsg = ""
			res, err = chat.SendMessage(ctx, funcResponses...)
			if err != nil {
				fmt.Fprintf(w, "data: {\"error\": \"%s\"}\n\n", err.Error())
				flusher.Flush()
				return
			}
			continue
		}

		// No function calls, stream the text response
		responseText := res.Text()
		fullResponse = responseText

		// Stream the response character by character for real-time effect
		for _, char := range responseText {
			chunk := map[string]any{
				"id":      "chatcmpl-" + fmt.Sprintf("%d", time.Now().UnixNano()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   model,
				"choices": []map[string]any{
					{
						"index": 0,
						"delta": map[string]string{
							"content": string(char),
						},
						"finish_reason": nil,
					},
				},
			}
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		break
	}

	// Send final chunk
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	writeDebugResponse(fullResponse)

	mu.Lock()
	sessions["openai-stream"] = chat.History(false)
	mu.Unlock()

	logMsg("<<< OpenAI Stream Complete | Resp: %.50s...", fullResponse)
}

// --- GEMINI STREAMING ---

func handleStream(w http.ResponseWriter, r *http.Request) {
	var reqBody struct {
		Contents      []map[string]any `json:"contents"`
		CachedContent string           `json:"cachedContent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid request", 400)
		return
	}

	// Extract user message from contents
	userMsg := ""
	for _, content := range reqBody.Contents {
		if role, ok := content["role"].(string); ok && role == "user" {
			if parts, ok := content["parts"].([]any); ok && len(parts) > 0 {
				if part, ok := parts[0].(map[string]any); ok {
					if text, ok := part["text"].(string); ok {
						userMsg = text
					}
				}
			}
		}
	}

	// Extract model from URL
	path := r.URL.Path
	model := DefaultModel
	if strings.Contains(path, "/models/") {
		parts := strings.Split(path, "/models/")
		if len(parts) > 1 {
			modelPart := strings.Split(parts[1], ":")[0]
			if modelPart != "" {
				model = modelPart
			}
		}
	}

	logMsg(">>> Gemini Stream | Model: %s | Msg: %.50s...", model, userMsg)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	config := &genai.GenerateContentConfig{
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdBlockNone},
			{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdBlockNone},
		},
	}

	activeCID := reqBody.CachedContent
	if activeCID == "" {
		activeCID = cacheName
	}
	if activeCID != "" {
		config.CachedContent = activeCID
	}

	chat, err := client.Chats.Create(ctx, model, config, nil)
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\": \"%s\"}\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Use streaming with Go 1.23+ range over iterator
	fullResponse := ""

	for resp, err := range chat.SendMessageStream(ctx, genai.Part{Text: userMsg}) {
		if err != nil {
			fmt.Fprintf(w, "data: {\"error\": \"%s\"}\n\n", err.Error())
			flusher.Flush()
			break
		}

		text := resp.Text()
		fullResponse += text

		// Send in Gemini format
		chunk := map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]string{{"text": text}},
						"role":  "model",
					},
				},
			},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	writeDebugResponse(fullResponse)
	logMsg("<<< Gemini Stream Complete | Resp: %.50s...", fullResponse)
}

func handleAssets(w http.ResponseWriter, r *http.Request) {
	// Strip the leading "/" to get the path relative to the embed
	path := strings.TrimPrefix(r.URL.Path, "/")

	// Read from embedded filesystem
	data, err := assetsFS.ReadFile("web/" + path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Set content type based on extension
	ext := filepath.Ext(path)
	switch ext {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript")
	case ".css":
		w.Header().Set("Content-Type", "text/css")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	w.Write(data)
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	// Parse the embedded template
	tmpl, err := template.New("index").Parse(indexHTML)
	if err != nil {
		http.Error(w, "Template parsing error: "+err.Error(), 500)
		return
	}

	// Prepare template data
	data := TemplateData{
		CacheName:  cacheName,
		CacheModel: cacheModel,
		ServerPort: serverPort,
		MCPPath:    filepath.Join(serverHome, "cmd/mcp/main.go"),
	}

	// Execute template to buffer first to catch errors
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		http.Error(w, "Template execution error: "+err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(buf.Bytes())
}

func handleFiles(w http.ResponseWriter, r *http.Request) {
	// Support ?path= query for subdirectories
	subPath := r.URL.Query().Get("path")
	targetDir := projectRoot
	if subPath != "" {
		// Sanitize path to prevent directory traversal
		cleanPath := filepath.Join(projectRoot, filepath.Clean(subPath))
		if !strings.HasPrefix(cleanPath, projectRoot) {
			http.Error(w, "Access denied", 403)
			return
		}
		targetDir = cleanPath
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var files []string
	for _, e := range entries {
		name := e.Name()
		// Skip hidden files and common non-essential directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			// Skip common large directories
			skip := map[string]bool{
				"node_modules": true, "vendor": true, ".git": true,
				"dist": true, "build": true, ".next": true, "target": true,
				"__pycache__": true, "venv": true, ".venv": true,
			}
			if skip[name] {
				continue
			}
			name += "/"
		}
		files = append(files, name)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"files": files})
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if r.Method == http.MethodPost {
		json.NewDecoder(r.Body).Decode(&req)
	}

	if req.Model == "" {
		req.Model = DefaultModel
	}
	if req.SessionID == "" {
		req.SessionID = "default"
	}

	// Log incoming request
	msgPreview := req.Message
	if len(msgPreview) > 50 {
		msgPreview = msgPreview[:50] + "..."
	}
	logMsg(">>> /chat | Model: %s | Session: %s | Search: %v | Msg: %s", req.Model, req.SessionID, req.UseSearch, msgPreview)

	mu.Lock()
	history := sessions[req.SessionID]
	mu.Unlock()

	// Determine if we can use a cache
	activeCID := ""
	if req.CacheID != "" {
		activeCID = req.CacheID
	} else if cacheName != "" {
		// Only use cache if models are compatible
		// Skip cache for: image generation, experimental, different model families
		isImageModel := strings.Contains(req.Model, "image")
		isDifferentFamily := !strings.HasPrefix(req.Model, strings.TrimSuffix(cacheModel, "-001"))
		if !isImageModel && !isDifferentFamily {
			activeCID = cacheName
		}
	}

	// Build the generate content config
	config := &genai.GenerateContentConfig{
		Temperature: genai.Ptr[float32](0.2),
		SafetySettings: []*genai.SafetySetting{
			{
				Category:  genai.HarmCategoryHarassment,
				Threshold: genai.HarmBlockThresholdBlockNone,
			},
			{
				Category:  genai.HarmCategoryHateSpeech,
				Threshold: genai.HarmBlockThresholdBlockNone,
			},
			{
				Category:  genai.HarmCategorySexuallyExplicit,
				Threshold: genai.HarmBlockThresholdBlockNone,
			},
			{
				Category:  genai.HarmCategoryDangerousContent,
				Threshold: genai.HarmBlockThresholdBlockNone,
			},
		},
	}

	// Initialize tools slice
	var tools []*genai.Tool

	// Add Google Search grounding if requested
	if req.UseSearch {
		tools = append(tools, &genai.Tool{
			GoogleSearch: &genai.GoogleSearch{},
		})
	}

	// Note: Gemini API does not allow tools with CachedContent
	// Skip cache when agentic mode or search is enabled (both use tools)
	if activeCID != "" && !req.UseAgentic && !req.UseSearch {
		config.CachedContent = activeCID
	}

	// Add file tools only when agentic mode is enabled
	if req.UseAgentic {
		fileTools := []*genai.FunctionDeclaration{
			{
				Name:        "write_file",
				Description: "Write or create a file with the specified content",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"path":    {Type: genai.TypeString, Description: "Relative path to the file"},
						"content": {Type: genai.TypeString, Description: "Content to write to the file"},
					},
					Required: []string{"path", "content"},
				},
			},
		}

		// Add read tools only when not using cache (cache already has file contents)
		if activeCID == "" {
			fileTools = append(fileTools, &genai.FunctionDeclaration{
				Name:        "list_files",
				Description: "List files in the current directory or subdirectory",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"path": {Type: genai.TypeString, Description: "Relative path to list (use '.' for current)"},
					},
				},
			}, &genai.FunctionDeclaration{
				Name:        "read_file",
				Description: "Read the contents of a specific file",
				Parameters: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"path": {Type: genai.TypeString, Description: "Relative path to the file"},
					},
					Required: []string{"path"},
				},
			})
		}

		tools = append(tools, &genai.Tool{
			FunctionDeclarations: fileTools,
		})
	}

	// Set tools if any were configured
	if len(tools) > 0 {
		config.Tools = tools
	}

	// Create a chat session with history
	chat, err := client.Chats.Create(ctx, req.Model, config, history)
	if err != nil {
		http.Error(w, "Failed to create chat: "+err.Error(), 500)
		return
	}

	// Initialize empty (will set fallback at end if needed)
	finalResponse := ""
	var toolLogs []string
	var images []ImageData
	var requestCost float64
	var promptToks, respToks, totalToks int

	if req.Message == "" {
		req.Message = "Hello"
	}

	fmt.Printf("[DEBUG] Sending Message: Model=%s, CacheID=%s, HistoryCount=%d\n", req.Model, activeCID, len(history))

	// Send the message
	res, err := chat.SendMessage(ctx, genai.Part{Text: req.Message})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// UsageMetadata accumulates the total for the context but we want the delta for this request if possible.
	if res.UsageMetadata != nil {
		promptToks = int(res.UsageMetadata.PromptTokenCount)
		respToks = int(res.UsageMetadata.CandidatesTokenCount)
		totalToks = int(res.UsageMetadata.TotalTokenCount)
	}

	if len(res.Candidates) > 0 {
		fmt.Printf("[DEBUG] FinishReason: %s\n", res.Candidates[0].FinishReason)
	}

	fmt.Printf("[DEBUG] Initial Response: Candidates=%d, Tokens=%d\n", len(res.Candidates), totalToks)

	for {
		requestCost += calculateCost(req.Model, res)
		if len(res.Candidates) == 0 || res.Candidates[0].Content == nil {
			break
		}

		// Check for function calls
		funcCalls := res.FunctionCalls()
		if len(funcCalls) > 0 {
			var funcResponses []genai.Part
			for _, funcCall := range funcCalls {
				toolName := funcCall.Name
				toolLogs = append(toolLogs, fmt.Sprintf("Executed: %s", toolName))

				var funcResult map[string]any
				args := funcCall.Args

				if toolName == "list_files" {
					p, _ := args["path"].(string)
					funcResult = toolListFiles(p)
				} else if toolName == "read_file" {
					p, _ := args["path"].(string)
					funcResult = toolReadFile(p)
				} else if toolName == "write_file" {
					p, _ := args["path"].(string)
					c, _ := args["content"].(string)
					funcResult = toolWriteFile(p, c)
				} else {
					funcResult = map[string]any{"error": "unknown tool"}
				}

				funcResponses = append(funcResponses, genai.Part{
					FunctionResponse: &genai.FunctionResponse{
						Name:     toolName,
						Response: funcResult,
					},
				})
			}

			res, err = chat.SendMessage(ctx, funcResponses...)
			if err != nil {
				finalResponse = "Error after tool execution: " + err.Error()
				break
			}
			// Update tokens and costs for the follow-up response
			if res.UsageMetadata != nil {
				// Each turn's candidates count should be added
				respToks += int(res.UsageMetadata.CandidatesTokenCount)
				totalToks = int(res.UsageMetadata.TotalTokenCount)
			}
			fmt.Printf("[DEBUG] Tool Return: Candidates=%d, totalToks=%d\n", len(res.Candidates), totalToks)
			continue
		}

		// Extract text and images from response
		finalResponse = res.Text()

		// Check for image data in response parts
		if len(res.Candidates) > 0 && res.Candidates[0].Content != nil {
			for _, part := range res.Candidates[0].Content.Parts {
				if part.InlineData != nil && part.InlineData.Data != nil {
					images = append(images, ImageData{
						MimeType: part.InlineData.MIMEType,
						Data:     base64.StdEncoding.EncodeToString(part.InlineData.Data),
					})
				}
			}
		}
		break
	}

	finalResponse = strings.TrimSpace(finalResponse)
	if finalResponse == "" && len(toolLogs) == 0 && len(images) == 0 {
		finalResponse = "[System Warning: Model returned empty content. This may be a safety block or API glitch.]"
	} else if finalResponse == "" && len(toolLogs) > 0 {
		// If we executed tools but got no final text, avoiding making it look like an error
		finalResponse = fmt.Sprintf("[Executed %d tool(s) but model provided no summary.]", len(toolLogs))
	} else if finalResponse == "" && len(images) > 0 {
		finalResponse = fmt.Sprintf("[Generated %d image(s)]", len(images))
	}

	mu.Lock()
	sessions[req.SessionID] = chat.History(false)
	totalCost += requestCost
	mu.Unlock()

	// Log response
	respPreview := finalResponse
	if len(respPreview) > 50 {
		respPreview = respPreview[:50] + "..."
	}
	logMsg("<<< /chat | Tokens: %din/%dout (%d total) | Tools: %d | Images: %d | Cost: $%.6f | Resp: %s",
		promptToks, respToks, totalToks, len(toolLogs), len(images), requestCost, strings.ReplaceAll(respPreview, "\n", " "))

	writeDebugResponse(finalResponse)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChatResponse{
		Text:           finalResponse,
		Images:         images,
		ToolCalls:      toolLogs,
		PromptTokens:   promptToks,
		ResponseTokens: respToks,
		TotalTokens:    totalToks,
		Cost:           requestCost,
		TotalCost:      totalCost,
	})
}

func toolListFiles(relPath string) map[string]any {
	if relPath == "" {
		relPath = "."
	}
	// Always stay within projectRoot
	cleanPath := filepath.Join(projectRoot, filepath.Clean(relPath))
	if !strings.HasPrefix(cleanPath, projectRoot) {
		return map[string]any{"error": "Access denied: outside project root"}
	}

	entries, err := os.ReadDir(cleanPath)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}
	return map[string]any{"files": files}
}

func toolReadFile(relPath string) map[string]any {
	// Always stay within projectRoot
	cleanPath := filepath.Join(projectRoot, filepath.Clean(relPath))
	if !strings.HasPrefix(cleanPath, projectRoot) {
		return map[string]any{"error": "Access denied: outside project root"}
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if info.Size() > 1000000 { // 1MB limit for tools
		return map[string]any{"error": "File too large"}
	}
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	return map[string]any{"content": string(content)}
}

func toolWriteFile(relPath, content string) map[string]any {
	// Always stay within projectRoot
	cleanPath := filepath.Join(projectRoot, filepath.Clean(relPath))
	if !strings.HasPrefix(cleanPath, projectRoot) {
		return map[string]any{"error": "Access denied: outside project root"}
	}

	// Create parent directories if needed
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return map[string]any{"error": "Failed to create directory: " + err.Error()}
	}

	if err := os.WriteFile(cleanPath, []byte(content), 0644); err != nil {
		return map[string]any{"error": err.Error()}
	}

	logMsg("[TOOL] write_file: %s (%d bytes)", relPath, len(content))
	return map[string]any{"status": "OK", "path": relPath, "bytes_written": len(content)}
}

func handleReset(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	sessions = make(map[string][]*genai.Content)
	mu.Unlock()
	fmt.Fprint(w, "All sessions cleared.")
}

func calculateCost(modelName string, resp *genai.GenerateContentResponse) float64 {
	var rates struct{ In, Out float64 }
	found := false
	for modelKey, r := range modelCosts {
		if modelName == modelKey || strings.HasPrefix(modelName, modelKey) {
			rates = r
			found = true
			break
		}
	}

	if !found || (rates.In == 0 && rates.Out == 0) {
		return 0
	}
	if resp.UsageMetadata == nil {
		return 0
	}

	inCost := (float64(resp.UsageMetadata.PromptTokenCount) / 1000000.0) * rates.In
	outCost := (float64(resp.UsageMetadata.CandidatesTokenCount) / 1000000.0) * rates.Out
	return inCost + outCost
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	type ModelData struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Cost string `json:"cost"`
	}

	var models []ModelData

	// Use the new iterator API
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			break
		}
		// Check if model supports generateContent by looking at SupportedActions
		supportsGenerate := false
		for _, action := range m.SupportedActions {
			if action == "generateContent" {
				supportsGenerate = true
				break
			}
		}

		if supportsGenerate {
			id := strings.TrimPrefix(m.Name, "models/")

			// Skip problematic experimental models
			if strings.Contains(id, "image-generation") || 
			   strings.Contains(id, "-exp") || 
			   strings.Contains(id, "experimental") ||
			   strings.Contains(id, "2.0-flash-exp") ||
			   strings.Contains(id, "2.0-pro-exp") {
				continue
			}

			costStr := "Price: Variable"

			// Try exact match or prefix match for pricing
			for modelKey, rates := range modelCosts {
				if id == modelKey || strings.HasPrefix(id, modelKey) {
					if rates.In == 0 && rates.Out == 0 {
						costStr = "Price: Free (Beta)"
					} else {
						costStr = fmt.Sprintf("$%.2f/1M tokens", rates.In)
					}
					break
				}
			}

			models = append(models, ModelData{
				ID:   id,
				Name: id,
				Cost: costStr,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"models": models})
}

func ListModels(client *genai.Client) {
	for m, err := range client.Models.All(ctx) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Model: %s\n", m.Name)
	}
}
