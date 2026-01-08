# Troubleshooting Guide

## Common Issues and Solutions

### 1. TLS/Certificate Errors (macOS)

**Error:**
```
tls: failed to verify certificate: x509: OSStatus -26276
```

**Solution:**

Set environment variables:
```bash
export SSL_CERT_FILE="/etc/ssl/cert.pem"
export SSL_CERT_DIR="/etc/ssl/certs"
./server -cache .
```

**Why this happens:**
- macOS uses a different certificate store than Go expects
- Go's `crypto/tls` package looks for certificates in `/etc/ssl/`
- Setting these env vars points Go to the correct certificate location

**Platforms affected:**
- macOS (needs fix)
- Linux (works out of box)
- Windows (works out of box)

---

### 2. Port Already in Use

**Error:**
```
listen tcp :8080: bind: address already in use
```

**Solution:**

Kill the existing process:
```bash
lsof -ti:8080 | xargs kill -9
```

Or use a different port:
```bash
./server -port :8081 -cache .
```

---

### 3. Cache Creation Fails

**Error:**
```
Error 404: model not found for createCachedContent
```

**Solution:**

Some models don't support caching. Use a supported model:
- gemini-2.5-flash (supports caching)
- gemini-1.5-flash (supports caching)
- gemini-1.5-pro (supports caching)
- gemini-3.0-flash (no cache support)

```bash
./server -cache . -model gemini-2.5-flash
```

---

### 4. Rate Limit Errors

**Error:**
```
Error 429: You have sent too many requests
```

**Solution:**

Wait a few minutes or:
- Use an existing cache ID instead of creating new ones
- Reduce request frequency
- Check your Google AI Studio quota

---

### 5. API Key Issues

**Error:**
```
API key not valid
```

**Solution:**

Set your API key:
```bash
export GEMINI_API_KEY=your_api_key_here
```

Or create a `.env` file:
```
GEMINI_API_KEY=your_api_key_here
```

Get a key from: https://aistudio.google.com/apikey

---

### 6. Web UI Not Loading

**Symptoms:**
- Blank page
- 404 errors
- Old theme showing

**Solutions:**

1. **Hard refresh browser:**
   - Mac: `Cmd + Shift + R`
   - Windows/Linux: `Ctrl + Shift + R`

2. **Server not rebuilt:**
   ```bash
   go build -o server main.go
   pkill -f "./server"
   ./server -cache-id <your-cache>
   ```

3. **Check if server is running:**
   ```bash
   lsof -i:8080
   ```

---

### 7. Tool Activity Not Working

**Issue:**
- Agentic mode not executing tools
- Cache + tools conflict

**Solution:**

Tools must be defined when creating the cache:
```bash
# Create cache with tools included
./server -cache . -model gemini-2.5-flash

# This creates a cache with file tools built in
# Then use that cache ID for future requests
```

**Note:** Google Search and custom tools are mutually exclusive in cached content.

---

### 8. Mobile View Not Responsive

**Issue:**
- Sidebar not hiding on mobile
- Layout broken on small screens

**Solution:**

1. Hard refresh to clear CSS cache
2. Check browser width (breakpoint is 768px)
3. Click the grid menu icon (top-left) to toggle sidebar

---

## Still Having Issues?

### Debug Mode

Enable debug logging:
```bash
./server -debug -cache .
```

This saves full responses to `debug_last_response.txt`

### Check Logs

View server logs:
```bash
tail -f logs/server_$(date +%Y-%m-%d).log
```

### Verify Installation

```bash
# Check Go version (needs 1.21+)
go version

# Check if binary exists
ls -lh ./server

# Check API key is set
echo $GEMINI_API_KEY
```

### GitHub Issues

Report bugs at: https://github.com/hugopalma17/Gemini-Cacher-and-MCP-Proxy/issues

Include:
- OS and version
- Go version
- Error messages
- Server logs
- Steps to reproduce
