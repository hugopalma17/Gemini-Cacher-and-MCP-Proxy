# Antigravity Brain - Development History

This document summarizes the collaborative development session between a human developer and an AI assistant (Claude) working on the Antigravity Brain project, a Gemini API caching proxy server.

## Project Overview

**Goal**: Build a Go server that caches context for the Gemini API to reduce token usage and costs, with a web UI for interaction and compatibility endpoints for IDE integration.

## Timeline of Development

### Phase 1: Initial Fixes

**Problem**: The original `main.go` had syntax errors preventing compilation.

- **Human**: Identified the file as a "caching tool for Gemini" and requested syntax fixes.
- **AI**: Found and fixed two syntax errors:
  - Extra closing brace `}` in embedded CSS
  - Stray `go` text in embedded JavaScript

### Phase 2: SDK Migration

**Problem**: Code was written for an older version of the `google.golang.org/genai` SDK.

- **Human**: Requested update for the "new SDK"
- **AI**: Migrated 18+ API calls to v1.40.0 SDK, including:
  - `genai.NewClient` initialization changes
  - `client.Caches.Create` replacing `client.CreateCachedContent`
  - Iterator-based model listing (`client.Models.All`)
  - Updated safety settings constants
  - Chat session creation changes

### Phase 3: Code Separation and Embedding

**Problem**: HTML/CSS/JS was embedded directly in Go source, making editing difficult.

- **Human**: Suggested separating HTML to external files and embedding on compile, also removing CDN dependencies.
- **AI**: 
  - Extracted `web/index.html` with Go template placeholders
  - Downloaded and embedded external JS/CSS (marked.js, prism.js, etc.) to `web/assets/`
  - Used `go:embed` directives for single-binary compilation

### Phase 4: Dynamic Model Loading

**Problem**: Model list was hardcoded and could become outdated.

- **Human**: Requested dynamic model list from API, with cache model as default selection.
- **AI**: Implemented `/models` endpoint and JavaScript to populate dropdown dynamically, with intelligent default selection matching the cache model.

### Phase 5: Google Search Integration

**Problem**: No way to get real-time information.

- **Human**: Asked if the new SDK supports search, and requested it work for both WebUI and MCP endpoint.
- **AI**: Implemented `GoogleSearch` tool integration with a toggle checkbox in the UI.

**Critical Learning**: During testing, the server crashed. The human's request for testing revealed that Google Search (being a tool) conflicts with cached content. AI fixed this by skipping cache when search is enabled.

### Phase 6: Feature Expansion

Based on comparing with a Node.js reference implementation (`node/serverv2.js`):

- **Human**: Provided a feature wishlist:
  - Streaming responses
  - Write file tool
  - OpenAI compatibility
  - Clean mode as default
  - Verbose/debug logging
  - Agentic toggle for WebUI
  - IDE setup tutorials

- **AI**: Implemented all requested features including:
  - `/v1/chat/completions` and `/v1/models` OpenAI-compatible endpoints
  - Server-Sent Events streaming
  - File tools (`list_files`, `read_file`, `write_file`)
  - `-cache` flag for explicit caching (clean mode default)
  - Comprehensive help modal with IDE setup guides

### Phase 7: UI Polish

- **Human**: Requested removal of emojis, use of Unicons instead, professional README.
- **AI**: Replaced all emojis with Unicons, rewrote README with clear documentation.

- **Human**: Requested WebUI improvements:
  - Collapsible file tree with lazy loading
  - File type icons
  - Selected file "pills" above input
  - Clipboard image paste support
  - Proper markdown/code rendering
  - Copy buttons on code blocks

- **AI**: Implemented all UI features.

### Phase 8: Testing and Debugging

**Critical Phase** - This is where human guidance was most valuable.

- **Human**: Offered to help test, caught that cache was being rebuilt unnecessarily.
- **AI**: Added `-cache-id` flag to reuse existing caches.

- **Human**: Noticed "1220 tokens out" on a supposedly empty response and said "we need to see the information in raw format being exchanged, im blind here"
- **AI**: Checked debug logs and found the SDK warning about `InlineData` parts being ignored.

**The Fix**: 
- Added `ImageData` struct for base64-encoded images
- Extract images from `res.Candidates[0].Content.Parts` 
- Return images in API response
- Frontend renders images from base64

**Result**: Image generation with `nano-banana-pro-preview` worked perfectly.

## Key Lessons Learned

### 1. API Limitations Aren't Always Documented Clearly

The Gemini API silently fails when you combine:
- Cached content with tools (including Google Search)
- The SDK logs a warning but doesn't error

**Human insight**: Testing revealed crashes that led to discovering this limitation.

### 2. Token Counts Tell a Story

When the human noticed "1220 tokens out" but empty response, it was a critical clue that data WAS being generated but not displayed. Without this observation, the image rendering bug might have been attributed to model failure rather than code oversight.

### 3. Raw Data Visibility is Essential

The human's request to "see the information in raw format" led directly to finding the `InlineData` warning in logs, which revealed that images were being generated but ignored by `res.Text()`.

### 4. Iterative Testing Catches Edge Cases

- First weather test: Server crashed (tools + cache conflict)
- First image test: 1220 tokens, no display (InlineData ignored)
- Both were caught through real testing, not code review

## Contribution Summary

### Human Contributions

| Area | Contribution |
|------|--------------|
| Architecture | Decided on file separation, embedding strategy |
| Features | Prioritized feature list from Node.js reference |
| UX | Specified UI requirements (pills, icons, collapsible tree) |
| Testing | Caught cache rebuild issue, token count anomaly |
| Debugging | Requested raw data visibility that found the image bug |
| Direction | Kept focus on practical functionality over perfection |

### AI Contributions

| Area | Contribution |
|------|--------------|
| Implementation | Wrote all Go code, HTML/CSS/JS |
| SDK Migration | Translated old API calls to new SDK patterns |
| Bug Fixes | Fixed syntax errors, SDK compatibility, image extraction |
| Documentation | Wrote README, this history document |
| Testing | Executed browser-based testing, ran endpoint scripts |

## Final State

The project now includes:

- Go server with context caching for Gemini API
- WebUI with file browser, model selection, search/agentic toggles
- Image generation support with proper rendering
- OpenAI-compatible endpoints for IDE integration
- Comprehensive logging and debugging
- Single-binary deployment with embedded assets

## Files Modified/Created

```
main.go              - Core server (heavily modified)
web/index.html       - Web interface (new)
web/assets/*         - Embedded JS/CSS libraries (new)
README.md            - Documentation (rewritten)
test_endpoints.sh    - API testing script (new)
history.md           - This file (new)
.gitignore           - Updated for web/ directory
```

## Conclusion

This project demonstrates effective human-AI collaboration where:

- The human provided vision, priorities, and critical debugging insights
- The AI provided implementation speed and technical breadth
- Testing in real conditions caught issues that code review missed
- The "1220 tokens" observation was the key breakthrough for image support

The most valuable human contributions were not about writing code, but about knowing what to look for when something doesn't work as expected.

