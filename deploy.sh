#!/bin/bash
# =============================================================================
# Go Alert Web3 BNB - Deploy Script
# Docker & Deployment Operations (use Makefile for local dev)
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
PORT=8080

# Helper functions
log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[OK]${NC} $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

header() {
    echo -e "${BLUE}=============================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}=============================================${NC}"
}

# Get GitHub username
get_github_user() {
    command -v gh &>/dev/null && gh api user --jq '.login' 2>/dev/null || echo ""
}

# Ensure buildx builder
ensure_builder() {
    if ! docker buildx inspect go-alert-builder &>/dev/null; then
        log_info "Creating buildx builder..."
        docker buildx create --name go-alert-builder --use --driver docker-container
    else
        docker buildx use go-alert-builder 2>/dev/null || true
    fi
}

# Ensure .env exists
ensure_env() {
    if [ ! -f ".env" ]; then
        if [ -f ".env.example" ]; then
            log_info "Creating .env from .env.example..."
            cp .env.example .env
            log_warn "Please edit .env with your configuration"
        else
            log_error ".env file not found"
            exit 1
        fi
    fi
}

# ============================================================================
# Commands
# ============================================================================

cmd_check() {
    header "Checking Prerequisites"
    local ok=1
    
    command -v docker &>/dev/null && log_success "Docker" || { log_error "Docker not found"; ok=0; }
    docker buildx version &>/dev/null && log_success "Docker Buildx" || log_warn "Buildx not available"
    command -v go &>/dev/null && log_success "Go $(go version | awk '{print $3}')" || log_warn "Go not found (ok for Docker builds)"
    command -v gh &>/dev/null && log_success "GitHub CLI" || log_warn "gh not found (needed for push)"
    
    [ $ok -eq 1 ] && log_success "All checks passed!"
}

cmd_build() {
    local tag=${1:-latest}
    header "Building Docker Image: $IMAGE_NAME:$tag"
    
    ensure_builder
    docker buildx build --platform linux/amd64 -t "$IMAGE_NAME:$tag" --load .
    log_success "Built: $IMAGE_NAME:$tag"
}

cmd_push() {
    local tag=${1:-latest}
    local github_user=$(get_github_user)
    
    [ -z "$github_user" ] && { log_error "Run: gh auth login"; exit 1; }
    
    header "Pushing to $REGISTRY/$github_user/$IMAGE_NAME:$tag"
    
    ensure_builder
    log_info "Logging in to ghcr.io..."
    gh auth token | docker login ghcr.io -u "$github_user" --password-stdin
    
    docker buildx build --platform linux/amd64 \
        -t "$REGISTRY/$github_user/$IMAGE_NAME:$tag" \
        -t "$REGISTRY/$github_user/$IMAGE_NAME:latest" \
        --push .
    
    log_success "Pushed: $REGISTRY/$github_user/$IMAGE_NAME:$tag"
}

cmd_run() {
    header "Starting All Services (foreground)"
    ensure_env
    export DOCKER_BUILDKIT=1
    docker compose up --build
}

cmd_up() {
    header "Starting All Services (background)"
    ensure_env
    export DOCKER_BUILDKIT=1
    docker compose up --build -d
    
    log_success "Services started!"
    echo ""
    echo -e "${GREEN}Settings UI:${NC} http://localhost:8080"
    echo ""
    log_info "View logs: $0 logs"
    log_info "Stop: $0 down"
}

cmd_down() {
    header "Stopping Services"
    docker compose down
    log_success "Services stopped"
}

cmd_logs() {
    local service=${1:-}
    [ -z "$service" ] && docker compose logs -f || docker compose logs -f "$service"
}

cmd_status() {
    header "Service Status"
    docker compose ps
}

cmd_restart() {
    header "Restarting Services"
    docker compose restart
    log_success "Services restarted"
}

cmd_scale() {
    local count=${1:-3}
    header "Scaling to $count instances"
    docker compose up -d --scale go-alert-web3-bnb=$count
    log_success "Scaled to $count instances"
}

cmd_clean() {
    header "Cleaning"
    docker compose down -v --rmi local 2>/dev/null || true
    docker buildx prune -f 2>/dev/null || true
    log_success "Cleaned"
}

cmd_deploy() {
    local tag=${1:-latest}
    local github_user=$(get_github_user)
    
    [ -z "$github_user" ] && { log_error "Run: gh auth login"; exit 1; }
    
    cmd_push "$tag"
    
    if command -v coolify &>/dev/null; then
        header "Deploying to Coolify"
        # Get existing app or show manual instructions
        local app_uuid=$(coolify app list --format json 2>/dev/null | jq -r ".[] | select(.name==\"$IMAGE_NAME\") | .uuid" 2>/dev/null)
        
        if [ -n "$app_uuid" ] && [ "$app_uuid" != "null" ]; then
            coolify app restart "$app_uuid"
            log_success "App restarted!"
        else
            log_warn "App not found in Coolify. Create it manually with:"
            echo "  Image: $REGISTRY/$github_user/$IMAGE_NAME:$tag"
            echo "  Port: $PORT"
        fi
    else
        log_success "Image pushed. Deploy manually to your platform."
        echo ""
        echo "Image: $REGISTRY/$github_user/$IMAGE_NAME:$tag"
    fi
}

cmd_help() {
    cat << EOF
Go Alert Web3 BNB - Deploy Script

Usage: $0 <command> [options]

Docker Commands:
  build [tag]       Build Docker image (default: latest)
  push [tag]        Build and push to GitHub Container Registry
  deploy [tag]      Push and deploy to Coolify (if available)

Run Commands:
  run               Run all services (foreground, with build)
  up                Run all services (background, with build)
  down              Stop all services
  restart           Restart all services
  logs [service]    View logs (service: go-alert-web3-bnb, settings-ui, redis)
  status            Show service status
  scale [n]         Scale alert bot to n instances (default: 3)

Utility:
  check             Check prerequisites
  clean             Remove containers, volumes, and cache
  help              Show this help

Examples:
  $0 up                     # Start in background
  $0 logs go-alert-web3-bnb # View alert bot logs
  $0 scale 3                # Scale to 3 instances
  $0 push v1.0.0            # Push tagged version
  $0 deploy                 # Push and deploy

For local development (build, test, lint), use: make help
EOF
}

# Main
case "${1:-help}" in
    check)   cmd_check ;;
    build)   cmd_build "$2" ;;
    push)    cmd_push "$2" ;;
    deploy)  cmd_deploy "$2" ;;
    run)     cmd_run ;;
    up)      cmd_up ;;
    down)    cmd_down ;;
    restart) cmd_restart ;;
    logs)    cmd_logs "$2" ;;
    status)  cmd_status ;;
    scale)   cmd_scale "$2" ;;
    clean)   cmd_clean ;;
    help|--help|-h) cmd_help ;;
    *) log_error "Unknown: $1"; echo ""; cmd_help; exit 1 ;;
esac
