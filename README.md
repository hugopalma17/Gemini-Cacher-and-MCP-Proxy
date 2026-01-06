# Gemini Context Caching Proxy

A Go server that proxies requests to Google's Gemini API with optional context caching. The server reduces token costs by caching your project files once and reusing them across multiple requests. It exposes OpenAI-compatible endpoints for integration with any IDE or tool.

**Version:** 1.2.0

## Requirements

- Go 1.21 or higher
- Google Gemini API key

## Installation

### Quick Install (Automated)

For users who want a one-command installation:

```bash
curl -fsSL https://raw.githubusercontent.com/hugopalma17/Gemini-Cacher-and-MCP-Proxy/main/install.sh | bash
```

This script will:
- Check for Go installation (downloads if needed)
- Download and install Go 1.23.0 if not present
- Download project dependencies
- Build the server binary
- Guide you through API key setup

### Manual Installation

Clone the repository and install dependencies:

```bash
git clone https://github.com/hugopalma17/Gemini-Cacher-and-MCP-Proxy.git
cd Gemini-Cacher-and-MCP-Proxy
go mod download
```

Set your API key:

```bash
export GEMINI_API_KEY=your_api_key_here
```

Alternatively, create a `.env` file in the project root:

```
GEMINI_API_KEY=your_api_key_here
```

## Compilation

Build the main server:

```bash
go build -o server main.go
```

Build the MCP bridge for Claude Desktop and Cursor:

```bash
go build -o mcp cmd/mcp/main.go
```

## Usage

The server runs in clean mode by default, which is stateless and does not build a cache.

### Basic Usage

```bash
./server
```

This starts the server on port 8080. Open `http://localhost:8080` in your browser for the web interface.

### Command Line Options

```
-port string      Port to run on (default ":8080")
-model string     Gemini model to use (default "gemini-2.0-flash")
-cache string     Path to cache; enables caching mode
-cache-id string  Use an existing cache ID directly
-list-models      List available models and exit
-debug            Save responses to debug_last_response.txt
-version          Show version and exit
```

### Examples

Start with context caching for a project:

```bash
./server -cache /path/to/project
```

Cache the current directory:

```bash
./server -cache .
```

Use a different model:

```bash
./server -model gemini-1.5-pro
```

Reuse an existing cache:

```bash
./server -cache-id cachedContents/abc123xyz
```

List available models:

```bash
./server -list-models
```

## API Endpoints

The server exposes multiple APIs for compatibility with different tools.

### OpenAI Compatible

These endpoints work with any tool that supports custom OpenAI base URLs.

| Endpoint | Description |
|----------|-------------|
| `POST /v1/chat/completions` | Chat completions with streaming support |
| `GET /v1/models` | List available models |

### Native Endpoints

| Endpoint | Description |
|----------|-------------|
| `POST /chat` | Native chat with tool calling and Google Search |
| `GET /files` | List files in project directory |
| `GET /models` | List Gemini models with pricing |
| `GET /status` | Server status and statistics |
| `POST /reset` | Clear session history |

### Native Chat Request

```json
{
  "session_id": "my-session",
  "model": "gemini-2.0-flash",
  "message": "Explain this code",
  "use_search": false
}
```

## IDE Integration

All integrations use the OpenAI-compatible endpoint at `http://localhost:8080/v1`.

### Cursor

Cursor's AI chat uses OpenAI-compatible endpoints. Configure it in Cursor settings:

1. Open Cursor Settings (Cursor menu > Settings > Cursor Settings)
2. Navigate to the AI/Model settings section
3. Configure:
   - **Provider**: OpenAI Compatible / Custom OpenAI
   - **Base URL**: `http://localhost:8080/v1`
   - **API Key**: Any value (e.g., `x`) - the proxy ignores this
   - **Model**: `gpt-4` (proxy translates to your configured Gemini model)
4. Save and restart Cursor if required
5. Ensure the proxy server is running on port 8080

**Note**: For MCP tools/extensions, you can use the MCP bridge at `cmd/mcp/main.go` by adding to `~/.cursor/mcp.json`:
```json
{
  "mcpServers": {
    "gemini-brain": {
      "command": "go",
      "args": ["run", "/path/to/cmd/mcp/main.go"]
    }
  }
}
```

### VS Code with Continue.dev

Edit `~/.continue/config.yaml` (or use Continue's settings UI):

```yaml
name: Gemini Proxy
version: 1.0.0
schema: v1
models:
  - name: Gemini
    provider: openai
    model: gpt-4
    apiBase: http://localhost:8080/v1
    apiKey: not-needed
```

### Zed Editor

Edit `~/.config/zed/settings.json`:

```json
{
  "assistant": {
    "default_model": {
      "provider": "openai",
      "model": "gpt-4"
    },
    "version": "2"
  },
  "language_models": {
    "openai": {
      "api_url": "http://localhost:8080/v1",
      "available_models": [
        { "name": "gpt-4", "max_tokens": 128000 }
      ]
    }
  }
}
```

### Antigravity

1. Open Antigravity settings (usually via menu or preferences)
2. Navigate to the AI/Model configuration section
3. Configure the following settings:
   - **Provider**: Select "OpenAI Compatible" or "Custom OpenAI"
   - **Base URL**: `http://localhost:8080/v1`
   - **API Key**: Enter any value (e.g., `x` or `not-needed`) - the proxy ignores this
   - **Model**: `gpt-4` (or any model name - the proxy will translate to your configured Gemini model)
4. Save the settings and restart Antigravity if required
5. Ensure the proxy server is running on port 8080 before using Antigravity

### Claude Desktop

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "gemini-brain": {
      "command": "/path/to/mcp"
    }
  }
}
```

## Context Caching

When started with `-cache`, the server uploads your project files to Google's Context Caching API. Subsequent requests reference the cache instead of sending the full content, reducing costs significantly.

### How It Works

1. The server scans the specified directory for source files
2. Files are compiled and uploaded to Google's cache
3. A cache ID is returned and used for all requests
4. The cache expires after 2 hours

### Supported File Types

`.go`, `.js`, `.ts`, `.py`, `.lua`, `.html`, `.css`, `.md`, `.json`, `.txt`, `.sh`, `.env`, `.yaml`

### Excluded Directories

`.git`, `node_modules`, `venv`, `dist`, `build`, `.next`, `target`, `out`, `vendor`

### Cost Comparison

Without caching, a 100k token project context costs approximately $0.01 per request. With caching, only the cache reference is sent, reducing costs to roughly $0.0001 per request after the initial upload.

## Logs

The server writes logs to `logs/server_YYYY-MM-DD.log`. Enable debug mode with `-debug` to save full responses to `debug_last_response.txt`.

## Project Structure

```
customgemini/
  main.go           Server implementation
  cmd/
    mcp/main.go     MCP bridge for Claude and Cursor
    ask/main.go     CLI tool for quick queries
  web/
    index.html      Web interface
    assets/         Static files (embedded at compile time)
  logs/             Server logs
```

## License

MIT
