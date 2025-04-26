#!/bin/bash

# This script dumps the content of all files tracked by Git AND untracked files
# (respecting .gitignore) into workspace_dump.txt in the current directory.

echo "Dumping tracked and untracked files to workspace_dump.txt..."

git ls-files --exclude-standard --others | while IFS= read -r filename; do
  echo "--- File: $filename ---"
  # Handle potential errors reading a file
  if cat "$filename"; then
    echo # Add newline after content only if cat succeeded
  else
    echo ">>> Error reading file: $filename <<<"
  fi
  echo "--- End: $filename ---"
  echo # Add blank line for separation
done > workspace_dump.txt

echo "Dump complete: workspace_dump.txt"