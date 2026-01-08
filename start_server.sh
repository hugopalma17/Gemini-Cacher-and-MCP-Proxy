#!/bin/bash

# Antigravity Brain Server Startup Script
# Fixes macOS certificate verification issues

cd "$(dirname "$0")"

# Set certificate paths for Go's crypto/tls
export SSL_CERT_FILE="/etc/ssl/cert.pem"
export SSL_CERT_DIR="/etc/ssl/certs"
export CURL_CA_BUNDLE="/etc/ssl/cert.pem"

# Get cache ID from arguments or use default
CACHE_ID="${1:-d16bguzftmfcmhwqo66gj4314re1lejg9etzwyaa}"

echo "🚀 Starting Antigravity Brain Server..."
echo "   Cache ID: $CACHE_ID"
echo "   SSL Cert: $SSL_CERT_FILE"
echo ""

# Start the server
exec ./server -cache-id "$CACHE_ID"
