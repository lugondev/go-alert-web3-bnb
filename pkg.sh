#!/bin/bash
# =============================================================================
# pkg.sh - Build & Publish Docker Images to GitHub Container Registry
# =============================================================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Config
IMAGE_NAME="go-alert-web3-bnb"
REGISTRY="ghcr.io"
PLATFORMS="linux/amd64,linux/arm64"

# Helper functions
log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# Get GitHub username from gh CLI
get_github_user() {
    if ! command -v gh &>/dev/null; then
        log_error "GitHub CLI (gh) not installed. Install: https://cli.github.com/"
    fi
    
    local user=$(gh api user --jq '.login' 2>/dev/null)
    if [ -z "$user" ]; then
        log_error "Not authenticated. Run: gh auth login"
    fi
    echo "$user"
}

# Get version from git or argument
get_version() {
    local version=${1:-}
    if [ -z "$version" ]; then
        version=$(git describe --tags --always --dirty 2>/dev/null || echo "latest")
    fi
    echo "$version"
}

# Ensure buildx builder exists
ensure_builder() {
    local builder_name="pkg-builder"
    
    if ! docker buildx inspect "$builder_name" &>/dev/null; then
        log_info "Creating buildx builder: $builder_name"
        docker buildx create --name "$builder_name" --driver docker-container --bootstrap
    fi
    docker buildx use "$builder_name"
}

# Login to GitHub Container Registry
ghcr_login() {
    local github_user=$1
    log_info "Logging in to $REGISTRY..."
    gh auth token | docker login "$REGISTRY" -u "$github_user" --password-stdin
}

# =============================================================================
# Commands
# =============================================================================

cmd_build() {
    local tag=$(get_version "$1")
    local github_user=$(get_github_user)
    local full_image="$REGISTRY/$github_user/$IMAGE_NAME"
    
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  Building: $full_image:$tag${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
    
    ensure_builder
    
    log_info "Building for linux/amd64 (local load)..."
    docker buildx build \
        --platform linux/amd64 \
        --tag "$full_image:$tag" \
        --tag "$full_image:latest" \
        --load \
        .
    
    log_success "Built: $full_image:$tag"
    log_success "Built: $full_image:latest"
    echo ""
    echo "Run locally: docker run -p 8080:8080 $full_image:$tag"
}

cmd_publish() {
    local tag=$(get_version "$1")
    local github_user=$(get_github_user)
    local full_image="$REGISTRY/$github_user/$IMAGE_NAME"
    
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  Publishing: $full_image:$tag${NC}"
    echo -e "${BLUE}  Platforms: $PLATFORMS${NC}"
    echo -e "${BLUE}========================================${NC}"
    echo ""
    
    ensure_builder
    ghcr_login "$github_user"
    
    log_info "Building and pushing multi-platform image..."
    docker buildx build \
        --platform "$PLATFORMS" \
        --tag "$full_image:$tag" \
        --tag "$full_image:latest" \
        --push \
        .
    
    log_success "Published: $full_image:$tag"
    log_success "Published: $full_image:latest"
    echo ""
    echo -e "${GREEN}Pull command:${NC}"
    echo "  docker pull $full_image:$tag"
    echo ""
    echo -e "${GREEN}Package URL:${NC}"
    echo "  https://github.com/$github_user/$IMAGE_NAME/pkgs/container/$IMAGE_NAME"
}

cmd_info() {
    local github_user=$(get_github_user)
    local version=$(get_version)
    
    echo ""
    echo -e "${BLUE}Package Info${NC}"
    echo "────────────────────────────────────"
    echo "Image:     $IMAGE_NAME"
    echo "Registry:  $REGISTRY"
    echo "User:      $github_user"
    echo "Version:   $version"
    echo "Platforms: $PLATFORMS"
    echo ""
    echo "Full image: $REGISTRY/$github_user/$IMAGE_NAME:$version"
}

cmd_help() {
    cat << 'EOF'

pkg.sh - Build & Publish Docker Images to GitHub Container Registry

Usage: ./pkg.sh <command> [version]

Commands:
  build [version]     Build Docker image locally (linux/amd64)
  publish [version]   Build and push multi-platform image to ghcr.io
  info                Show package info (image name, registry, user)
  help                Show this help

Version:
  If not specified, uses git tag/commit hash or 'latest'

Examples:
  ./pkg.sh build              # Build with auto version
  ./pkg.sh build v1.0.0       # Build with specific tag
  ./pkg.sh publish            # Publish with auto version
  ./pkg.sh publish v1.0.0     # Publish with specific tag
  ./pkg.sh info               # Show package info

Prerequisites:
  - Docker with buildx
  - GitHub CLI (gh) authenticated: gh auth login

EOF
}

# =============================================================================
# Main
# =============================================================================

case "${1:-help}" in
    build)   cmd_build "$2" ;;
    publish) cmd_publish "$2" ;;
    info)    cmd_info ;;
    help|--help|-h) cmd_help ;;
    *)
        log_error "Unknown command: $1"
        cmd_help
        exit 1
        ;;
esac
