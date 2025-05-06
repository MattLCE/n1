#!/bin/bash
set -e

echo "=== Testing Vault ID Edge Cases ==="
echo

# Create a temporary directory for the test
TEST_DIR=$(mktemp -d)
echo "Test directory: $TEST_DIR"

# Always rebuild the bosr binary to ensure we have the latest changes
BOSR_PATH="./bin/bosr"
echo "Building bosr binary..."
go build -o "$BOSR_PATH" ./cmd/bosr

# Create a vault in the test directory
ORIGINAL_PATH="$TEST_DIR/original.db"
echo "Creating vault at $ORIGINAL_PATH..."
$BOSR_PATH init "$ORIGINAL_PATH"

# Store a value in the vault
echo "Storing a value in the vault..."
$BOSR_PATH put "$ORIGINAL_PATH" "test_key" "test_value"

# Verify the value can be retrieved
echo "Verifying the value can be retrieved..."
VALUE=$($BOSR_PATH get "$ORIGINAL_PATH" "test_key")
if [ "$VALUE" != "test_value" ]; then
    echo "ERROR: Expected 'test_value', got '$VALUE'"
    exit 1
fi
echo "Value retrieved successfully"

# Open the vault to trigger automatic migration to UUID-based storage
echo "Opening the vault to trigger automatic migration..."
$BOSR_PATH open "$ORIGINAL_PATH"

# Create a symbolic link to the vault
SYMLINK_PATH="$TEST_DIR/symlink.db"
echo "Creating a symbolic link at $SYMLINK_PATH..."
ln -s "$ORIGINAL_PATH" "$SYMLINK_PATH"

# Verify the value can be retrieved through the symlink
echo "Verifying the value can be retrieved through the symlink..."
VALUE=$($BOSR_PATH get "$SYMLINK_PATH" "test_key")
if [ "$VALUE" != "test_value" ]; then
    echo "ERROR: Expected 'test_value', got '$VALUE'"
    exit 1
fi
echo "Value retrieved through symlink successfully"

# Move the vault to a new location
MOVED_PATH="$TEST_DIR/moved.db"
echo "Moving the vault to $MOVED_PATH..."
mv "$ORIGINAL_PATH" "$MOVED_PATH"

# Verify the value can be retrieved from the new location
echo "Verifying the value can be retrieved from the new location..."
VALUE=$($BOSR_PATH get "$MOVED_PATH" "test_key")
if [ "$VALUE" != "test_value" ]; then
    echo "ERROR: Expected 'test_value', got '$VALUE'"
    exit 1
fi
echo "Value retrieved from moved location successfully"

# Open the vault to verify it's accessible
echo "Opening the vault to verify it's accessible..."
$BOSR_PATH open "$MOVED_PATH"

# Verify the value can still be retrieved after migration
echo "Verifying the value can still be retrieved after migration..."
VALUE=$($BOSR_PATH get "$MOVED_PATH" "test_key")
if [ "$VALUE" != "test_value" ]; then
    echo "ERROR: Expected 'test_value', got '$VALUE'"
    exit 1
fi
echo "Value retrieved after migration successfully"

# Copy the vault to yet another location
COPIED_PATH="$TEST_DIR/copied.db"
echo "Copying the vault to $COPIED_PATH..."
cp "$MOVED_PATH" "$COPIED_PATH"

# Verify the value can be retrieved from the copied location
echo "Verifying the value can be retrieved from the copied location..."
VALUE=$($BOSR_PATH get "$COPIED_PATH" "test_key")
if [ "$VALUE" != "test_value" ]; then
    echo "ERROR: Expected 'test_value', got '$VALUE'"
    exit 1
fi
echo "Value retrieved from copied location successfully"

# Clean up
echo "Cleaning up..."
rm -rf "$TEST_DIR"

echo
echo "=== All tests passed! ==="