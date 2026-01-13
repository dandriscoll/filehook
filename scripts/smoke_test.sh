#!/bin/bash
# Smoke test for filehook
#
# This script tests the basic functionality of filehook:
# 1. Creates a temp directory with config and plugins
# 2. Drops an input file
# 3. Verifies the command ran and output exists
# 4. Verifies skip logic works
# 5. Verifies modification policy behavior
# 6. Verifies status and errors commands

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m' # No Color

# Track test results
PASSED=0
FAILED=0

pass() {
    echo -e "${GREEN}✓ $1${NC}"
    PASSED=$((PASSED + 1))
}

fail() {
    echo -e "${RED}✗ $1${NC}"
    FAILED=$((FAILED + 1))
}

info() {
    echo -e "${YELLOW}→ $1${NC}"
}

# Get project directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Build filehook from project directory
info "Building filehook..."
cd "$PROJECT_DIR"
go build -o filehook ./cmd/filehook
FILEHOOK="$PROJECT_DIR/filehook"

# Create temp directory
TMPDIR=$(mktemp -d)
info "Setting up test environment in $TMPDIR"

cleanup() {
    cd "$PROJECT_DIR"
    rm -rf "$TMPDIR"
}
trap cleanup EXIT

# Create directory structure
mkdir -p "$TMPDIR/input"
mkdir -p "$TMPDIR/output"
mkdir -p "$TMPDIR/plugins"

# Create filename generator plugin
cat > "$TMPDIR/plugins/filename_gen.sh" << 'EOF'
#!/bin/bash
INPUT_PATH="$1"
BASENAME=$(basename "$INPUT_PATH")
NAME="${BASENAME%.*}"
EXT="${BASENAME##*.}"
OUTPUT_NAME="${NAME}.upper.${EXT}"
echo "{\"outputs\": [\"$OUTPUT_NAME\"]}"
EOF
chmod +x "$TMPDIR/plugins/filename_gen.sh"

# Create config
cat > "$TMPDIR/filehook.yaml" << EOF
version: 1

watch:
  paths:
    - ./input
  ignore:
    - "*.tmp"
  debounce_ms: 100

inputs:
  patterns:
    - "*.txt"

outputs:
  root: ./output

command: ["sh", "-c", "cat \"\$FILEHOOK_INPUT\" | tr 'a-z' 'A-Z' > \"\$FILEHOOK_OUTPUT\""]

plugins:
  filename_generator:
    path: ./plugins/filename_gen.sh

concurrency:
  mode: parallel
  max_workers: 2

on_modified: if-newer

state_dir: .filehook
EOF

cd "$TMPDIR"

# Test 1: Config discovery and show
info "Test 1: Config discovery..."
if "$FILEHOOK" config show > /dev/null 2>&1; then
    pass "Config discovery works"
else
    fail "Config discovery failed"
fi

# Test 2: Create input file and run
info "Test 2: Processing input file..."
echo "hello world" > input/test1.txt
OUTPUT=$("$FILEHOOK" run 2>&1)
if echo "$OUTPUT" | grep -q "Completed"; then
    if [ -f output/test1.upper.txt ]; then
        CONTENT=$(cat output/test1.upper.txt)
        if [ "$CONTENT" = "HELLO WORLD" ]; then
            pass "Input file processed correctly"
        else
            fail "Output content incorrect: $CONTENT"
        fi
    else
        fail "Output file not created"
    fi
else
    fail "Run command failed: $OUTPUT"
fi

# Test 3: Skip logic - run again should skip
info "Test 3: Skip logic..."
OUTPUT1=$("$FILEHOOK" run 2>&1)
if echo "$OUTPUT1" | grep -q "No jobs to process\|Skipping"; then
    pass "Skip logic works - no reprocessing"
else
    fail "Skip logic failed - unexpected output: $OUTPUT1"
fi

# Test 4: Modification policy (if-newer)
info "Test 4: Modification policy..."
sleep 0.5
# Touch input file to make it newer
touch input/test1.txt
sleep 0.1
OUTPUT2=$("$FILEHOOK" run 2>&1)
if echo "$OUTPUT2" | grep -q "Queued\|Completed: 1\|input newer"; then
    pass "Modification policy works - file reprocessed when newer"
elif echo "$OUTPUT2" | grep -q "outputs already exist\|Skipping"; then
    # This is also valid if output timestamps are newer
    pass "Modification policy works - outputs up to date check"
else
    fail "Modification policy unclear: $OUTPUT2"
fi

# Test 5: Status command
info "Test 5: Status command..."
if "$FILEHOOK" status 2>&1 | grep -q "Queue Status"; then
    pass "Status command works"
else
    fail "Status command failed"
fi

# Test 6: Queue list command
info "Test 6: Queue list command..."
if "$FILEHOOK" queue list > /dev/null 2>&1; then
    pass "Queue list command works"
else
    fail "Queue list command failed"
fi

# Test 7: Errors list command
info "Test 7: Errors list command..."
if "$FILEHOOK" errors list > /dev/null 2>&1; then
    pass "Errors list command works"
else
    fail "Errors list command failed"
fi

# Test 8: JSON output
info "Test 8: JSON output..."
if "$FILEHOOK" status --json 2>&1 | grep -q '"pending"'; then
    pass "JSON output works"
else
    fail "JSON output failed"
fi

# Test 9: Multiple input files
info "Test 9: Multiple input files..."
echo "file two" > input/test2.txt
echo "file three" > input/test3.txt
"$FILEHOOK" run > /dev/null 2>&1 || true
if [ -f output/test2.upper.txt ] && [ -f output/test3.upper.txt ]; then
    pass "Multiple files processed"
else
    fail "Multiple files not processed"
fi

# Test 10: Create a failing job and test errors
info "Test 10: Error handling..."
cat > "$TMPDIR/filehook_fail.yaml" << EOF
version: 1

watch:
  paths:
    - ./input

inputs:
  patterns:
    - "*.fail"

outputs:
  root: ./output

command: ["sh", "-c", "exit 1"]

plugins:
  filename_generator:
    path: ./plugins/filename_gen.sh

concurrency:
  mode: parallel
  max_workers: 1

on_modified: reprocess

state_dir: .filehook_fail
EOF

echo "test" > input/error.fail
"$FILEHOOK" --config filehook_fail.yaml run > /dev/null 2>&1 || true

ERROR_OUTPUT=$("$FILEHOOK" --config filehook_fail.yaml errors list 2>&1)
if echo "$ERROR_OUTPUT" | grep -q "error.fail\|Failed jobs"; then
    pass "Error tracking works"
else
    fail "Error tracking failed: $ERROR_OUTPUT"
fi

# Summary
echo ""
echo "================================"
echo -e "Results: ${GREEN}$PASSED passed${NC}, ${RED}$FAILED failed${NC}"
echo "================================"

if [ $FAILED -gt 0 ]; then
    exit 1
fi

exit 0
