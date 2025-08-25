#!/bin/bash
#
# Setup script for installing git hooks for prometheus-oauth-gateway
# Run this script after cloning the repository to set up development hooks
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Get the root directory of the git repository
get_git_root() {
    git rev-parse --show-toplevel 2>/dev/null || {
        print_error "This script must be run from within a git repository"
        exit 1
    }
}

# Install pre-commit hook
install_pre_commit_hook() {
    local git_root=$(get_git_root)
    local hooks_dir="$git_root/.git/hooks"
    local pre_commit_hook="$hooks_dir/pre-commit"
    
    print_status "Installing pre-commit hook..."
    
    # Create the hook content
    cat > "$pre_commit_hook" << 'EOF'
#!/bin/sh
#
# Pre-commit hook for prometheus-oauth-gateway
# Performs code formatting, linting, and testing before allowing commits
#

set -e  # Exit on any error

echo "🔨 Running pre-commit checks..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo "${RED}[ERROR]${NC} $1"
}

# Check if required tools are installed
check_dependencies() {
    print_status "Checking dependencies..."
    
    if ! command -v go >/dev/null 2>&1; then
        print_error "Go is not installed or not in PATH"
        exit 1
    fi
    
    if ! command -v goimports >/dev/null 2>&1; then
        print_warning "goimports not found, installing..."
        go install golang.org/x/tools/cmd/goimports@latest
    fi
    
    if ! command -v golangci-lint >/dev/null 2>&1; then
        print_error "golangci-lint is not installed. Please install it first:"
        print_error "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin v1.54.2"
        exit 1
    fi
}

# Format Go code
format_code() {
    print_status "Formatting Go code..."
    
    # Find all Go files that are staged for commit
    STAGED_GO_FILES=$(git diff --cached --name-only --diff-filter=ACM | grep '\.go$' || true)
    
    if [ -z "$STAGED_GO_FILES" ]; then
        print_status "No Go files to format"
        return 0
    fi
    
    # Format with goimports
    for file in $STAGED_GO_FILES; do
        if [ -f "$file" ]; then
            print_status "Formatting $file"
            goimports -w "$file"
            git add "$file"  # Re-add the formatted file
        fi
    done
    
    print_success "Code formatting completed"
}

# Run linting
run_linting() {
    print_status "Running golangci-lint..."
    
    if ! golangci-lint run; then
        print_error "Linting failed! Please fix the issues above."
        exit 1
    fi
    
    print_success "Linting passed"
}

# Run tests
run_tests() {
    print_status "Running tests..."
    
    # Run tests with coverage
    if ! go test -race -v ./...; then
        print_error "Tests failed! Please fix failing tests before committing."
        exit 1
    fi
    
    print_success "All tests passed"
}

# Check for sensitive files
check_sensitive_files() {
    print_status "Checking for sensitive files..."
    
    # List of patterns to check for
    SENSITIVE_PATTERNS="
        \.env$
        \.env\..*
        .*\.pem$
        .*\.key$
        .*\.p12$
        .*\.jks$
        config.*\.yaml$
        config.*\.yml$
        config.*\.json$
        secrets.*
    "
    
    STAGED_FILES=$(git diff --cached --name-only)
    
    for pattern in $SENSITIVE_PATTERNS; do
        if echo "$STAGED_FILES" | grep -E "$pattern" >/dev/null 2>&1; then
            print_warning "Found potentially sensitive files matching pattern: $pattern"
            echo "$STAGED_FILES" | grep -E "$pattern"
            print_warning "Please ensure these files don't contain secrets!"
        fi
    done
}

# Main execution
main() {
    print_status "Starting pre-commit hook for prometheus-oauth-gateway"
    
    # Check dependencies first
    check_dependencies
    
    # Format code (this will modify files and re-add them to staging)
    format_code
    
    # Check for sensitive files
    check_sensitive_files
    
    # Run linting
    run_linting
    
    # Run tests
    run_tests
    
    print_success "✅ All pre-commit checks passed! Ready to commit."
    echo ""
}

# Run main function
main "$@"
EOF

    # Make it executable
    chmod +x "$pre_commit_hook"
    
    print_success "Pre-commit hook installed successfully!"
}

# Check dependencies
check_system_dependencies() {
    print_status "Checking system dependencies..."
    
    if ! command -v go >/dev/null 2>&1; then
        print_error "Go is not installed. Please install Go first."
        exit 1
    fi
    
    print_status "Installing/updating Go tools..."
    
    # Install goimports if not present
    if ! command -v goimports >/dev/null 2>&1; then
        print_status "Installing goimports..."
        go install golang.org/x/tools/cmd/goimports@latest
    fi
    
    # Check for golangci-lint
    if ! command -v golangci-lint >/dev/null 2>&1; then
        print_warning "golangci-lint is not installed."
        print_status "Installing golangci-lint..."
        
        # Install golangci-lint
        curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.54.2
        
        if ! command -v golangci-lint >/dev/null 2>&1; then
            print_error "Failed to install golangci-lint. Please install it manually:"
            print_error "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$(go env GOPATH)/bin v1.54.2"
            exit 1
        fi
    fi
    
    print_success "All dependencies are ready!"
}

# Main function
main() {
    echo "🔧 Setting up git hooks for prometheus-oauth-gateway"
    echo ""
    
    # Check if we're in a git repository
    get_git_root > /dev/null
    
    # Check and install dependencies
    check_system_dependencies
    
    # Install the pre-commit hook
    install_pre_commit_hook
    
    echo ""
    print_success "🎉 Setup completed!"
    print_status "The pre-commit hook is now active and will run on every commit."
    print_status "It will:"
    print_status "  • Format your Go code with goimports"
    print_status "  • Run golangci-lint to check code quality"
    print_status "  • Run all tests to ensure nothing is broken"
    print_status "  • Check for potentially sensitive files"
    echo ""
    print_status "To bypass the hook (not recommended), use: git commit --no-verify"
}

# Run main function
main "$@"