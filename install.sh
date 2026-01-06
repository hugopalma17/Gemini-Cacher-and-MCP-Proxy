#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Progress indicator
show_progress() {
    local current=$1
    local total=$2
    local message=$3
    local percent=$((current * 100 / total))
    printf "\r${BLUE}[%3d%%]${NC} %s" "$percent" "$message"
}

echo -e "${GREEN}=== Antigravity Brain - Installation Script ===${NC}\n"
echo -e "${BLUE}Note:${NC} This script installs to your home directory. ${GREEN}No sudo required!${NC}\n"

# Step 1: Check if Go is installed
echo -e "${YELLOW}Step 1/5:${NC} Checking for Go installation..."
show_progress 1 5 "Checking Go version..."

if command -v go &> /dev/null; then
    GO_VERSION=$(go version | awk '{print $3}')
    echo -e "\r${GREEN}✓${NC} Go is installed: ${GO_VERSION}"
else
    echo -e "\r${YELLOW}⚠${NC} Go is not installed. Downloading Go..."
    
    # Check if we can write to $HOME
    if [ ! -w "$HOME" ]; then
        echo -e "${RED}✗${NC} Cannot write to $HOME. Please check permissions."
        exit 1
    fi
    
    # Detect OS and architecture
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)
    
    case $ARCH in
        x86_64) ARCH="amd64" ;;
        arm64|aarch64) ARCH="arm64" ;;
        *) echo -e "${RED}✗${NC} Unsupported architecture: $ARCH"; exit 1 ;;
    esac
    
    GO_VERSION="1.23.0"
    GO_TAR="go${GO_VERSION}.${OS}-${ARCH}.tar.gz"
    GO_URL="https://go.dev/dl/${GO_TAR}"
    
    echo -e "${BLUE}  →${NC} Downloading Go ${GO_VERSION} for ${OS}/${ARCH}..."
    echo -e "${BLUE}  →${NC} Installing to: ${GREEN}$HOME/go${NC} (no sudo needed)"
    
    # Download with progress
    if command -v curl &> /dev/null; then
        # Use curl with progress bar (# shows progress)
        echo -e "${BLUE}  →${NC} Progress:"
        curl -L -# -o /tmp/${GO_TAR} ${GO_URL}
        echo ""
    elif command -v wget &> /dev/null; then
        # Use wget with progress
        echo -e "${BLUE}  →${NC} Progress:"
        wget --progress=bar:force -O /tmp/${GO_TAR} ${GO_URL} 2>&1 | \
        while IFS= read -r line; do
            if [[ $line =~ ([0-9]+)% ]]; then
                percent="${BASH_REMATCH[1]}"
                printf "\r${BLUE}  →${NC} Progress: ${GREEN}%3s%%${NC}" "$percent"
            fi
        done
        echo ""
    else
        echo -e "\r${RED}✗${NC} Neither curl nor wget found. Please install one."
        exit 1
    fi
    
    echo -e "\r${GREEN}✓${NC} Go downloaded successfully"
    
    # Extract Go
    echo -e "${BLUE}  →${NC} Extracting Go to $HOME/go..."
    show_progress 3 5 "Extracting Go..."
    
    if [ -d "$HOME/go" ]; then
        rm -rf "$HOME/go"
    fi
    
    # Check if we can create the directory
    if ! mkdir -p "$HOME/go" 2>/dev/null; then
        echo -e "\r${RED}✗${NC} Cannot create $HOME/go. Check permissions."
        exit 1
    fi
    
    tar -C "$HOME" -xzf /tmp/${GO_TAR} 2>/dev/null
    if [ $? -ne 0 ]; then
        echo -e "\r${RED}✗${NC} Failed to extract Go. Check disk space and permissions."
        exit 1
    fi
    rm /tmp/${GO_TAR}
    
    # Add Go to PATH for this session
    export PATH="$HOME/go/bin:$PATH"
    export GOROOT="$HOME/go"
    
    echo -e "\r${GREEN}✓${NC} Go extracted to $HOME/go"
    
    # Persist PATH and GOROOT in shell config
    echo -e "${BLUE}  →${NC} Adding Go to PATH in shell configuration..."
    
    # Detect shell and config file
    SHELL_CONFIG=""
    if [ -n "$ZSH_VERSION" ] || [ -f "$HOME/.zshrc" ]; then
        SHELL_CONFIG="$HOME/.zshrc"
    elif [ -n "$BASH_VERSION" ] || [ -f "$HOME/.bashrc" ]; then
        SHELL_CONFIG="$HOME/.bashrc"
    else
        # Try to detect from $SHELL
        case "$SHELL" in
            *zsh) SHELL_CONFIG="$HOME/.zshrc" ;;
            *bash) SHELL_CONFIG="$HOME/.bashrc" ;;
            *) SHELL_CONFIG="$HOME/.profile" ;;
        esac
    fi
    
    # Check if already added
    if grep -q "go/bin" "$SHELL_CONFIG" 2>/dev/null; then
        echo -e "${GREEN}✓${NC} Go PATH already configured in $SHELL_CONFIG"
    else
        # Add to config file
        {
            echo ""
            echo "# Go installation (added by Antigravity Brain installer)"
            echo "export PATH=\"\$HOME/go/bin:\$PATH\""
            echo "export GOROOT=\"\$HOME/go\""
        } >> "$SHELL_CONFIG"
        echo -e "${GREEN}✓${NC} Added Go to $SHELL_CONFIG"
        echo -e "${YELLOW}  ⚠${NC} Run ${BLUE}source $SHELL_CONFIG${NC} or restart your terminal to use Go"
    fi
fi

# Step 2: Verify Go installation
show_progress 2 5 "Verifying Go installation..."
if ! go version &> /dev/null; then
    echo -e "\r${RED}✗${NC} Go is not in PATH. Please add it manually."
    exit 1
fi
echo -e "\r${GREEN}✓${NC} Go is ready"

# Step 3: Check for GEMINI_API_KEY
echo -e "\n${YELLOW}Step 2/5:${NC} Checking environment..."
show_progress 3 5 "Checking GEMINI_API_KEY..."

if [ -z "$GEMINI_API_KEY" ]; then
    if [ -f ".env" ] && grep -q "GEMINI_API_KEY" .env; then
        echo -e "\r${GREEN}✓${NC} Found GEMINI_API_KEY in .env file"
    else
        echo -e "\r${YELLOW}⚠${NC} GEMINI_API_KEY not set"
        echo -e "  ${BLUE}→${NC} You can set it now or create a .env file later"
        read -p "  Enter your GEMINI_API_KEY (or press Enter to skip): " API_KEY
        if [ ! -z "$API_KEY" ]; then
            echo "GEMINI_API_KEY=$API_KEY" > .env
            export GEMINI_API_KEY="$API_KEY"
            echo -e "${GREEN}✓${NC} API key saved to .env"
        fi
    fi
else
    echo -e "\r${GREEN}✓${NC} GEMINI_API_KEY is set"
fi

# Step 4: Download dependencies
echo -e "\n${YELLOW}Step 3/5:${NC} Downloading Go dependencies..."
show_progress 4 5 "Running go mod download..."

if [ ! -f "go.mod" ]; then
    echo -e "\r${YELLOW}⚠${NC} go.mod not found. Initializing module..."
    go mod init customgemini 2>/dev/null || true
fi

go mod download 2>&1 | while IFS= read -r line; do
    if [[ $line == *"go:"* ]]; then
        echo -e "\r${BLUE}  →${NC} $line"
    fi
done

echo -e "\r${GREEN}✓${NC} Dependencies downloaded"

# Step 5: Build the server
echo -e "\n${YELLOW}Step 4/5:${NC} Building server..."
show_progress 5 5 "Compiling main.go..."

if go build -o server main.go 2>&1; then
    echo -e "\r${GREEN}✓${NC} Server built successfully!"
    echo -e "\n${GREEN}=== Installation Complete ===${NC}"
    echo -e "\n${BLUE}Server binary:${NC} ./server"
    echo -e "${BLUE}Usage examples:${NC}"
    echo -e "  ${YELLOW}./server${NC}                    # Clean mode (no cache)"
    echo -e "  ${YELLOW}./server -cache .${NC}           # Cache current directory"
    echo -e "  ${YELLOW}./server -cache-id <id>${NC}      # Use existing cache"
    echo -e "  ${YELLOW}./server -port :8080${NC}        # Custom port"
    echo -e "\n${BLUE}Web UI:${NC} http://localhost:8080"
    echo -e "${BLUE}API:${NC} http://localhost:8080/v1"
else
    echo -e "\r${RED}✗${NC} Build failed. Check the error messages above."
    exit 1
fi

echo -e "\n${GREEN}Step 5/5:${NC} Done!"
echo -e "\n${YELLOW}Next steps:${NC}"
echo -e "  1. Set GEMINI_API_KEY in .env or export it"
echo -e "  2. Run: ${BLUE}./server${NC}"
echo -e "  3. Open: ${BLUE}http://localhost:8080${NC}"

