#!/bin/bash

# Default values
HOST=""
PORT=""
CONFIG_PATH="$HOME/.config/opencode/opencode.jsonc"
LOCAL_CONFIG="./opencode.jsonc"

# Parse arguments
while [[ "$#" -gt 0 ]]; do
    case $1 in
        --host) HOST="$2"; shift ;;
        --port) PORT="$2"; shift ;;
        --config) CONFIG_PATH="$2"; shift ;;
        --provider) PROVIDER="$2"; shift ;;
        --model) MODEL="$2"; shift ;;
        *) echo "Unknown parameter: $1"; exit 1 ;;
    esac
    shift
done

# Auto-detect host and port if not provided
if [ -z "$HOST" ] || [ -z "$PORT" ]; then
    echo "Error: --host and --port must be provided for automatic setup."
    exit 1
fi

if [ -z "$PROVIDER" ] || [ -z "$MODEL" ]; then
    echo "Usage: $0 --host <host> --port <port> --provider <provider> --model <model> [--config <path>]"
    exit 1
fi

# Send setup request
echo "Sending setup request to http://$HOST:$PORT/opencode/setup..."
RESPONSE=$(curl -s -X POST "http://$HOST:$PORT/opencode/setup" \
     -H "Content-Type: application/json" \
     -d "{\"provider\": \"$PROVIDER\", \"model\": \"$MODEL\"}")

echo "Response: $RESPONSE"

if [[ $RESPONSE == *"success"* ]]; then
    echo "Successfully set up OpenCode!"
else
    echo "Failed to set up OpenCode."
    exit 1
fi
