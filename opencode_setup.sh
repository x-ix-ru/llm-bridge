#!/bin/bash

# Default values
HOST="localhost"
PORT="8080"
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

if [ -z "$PROVIDER" ] || [ -z "$MODEL" ]; then
    echo "Usage: $0 --host <host> --port <port> --provider <provider> --model <model> [--config <path>]"
    exit 1
fi

# 1. Check if opencode is available (basic check via curl/network)
# In this context, we assume the user wants to setup the bridge for opencode.
# We'll just proceed to the setup.

# 2. Get config
if [ -f "$LOCAL_CONFIG" ]; then
    echo "Using local config: $LOCAL_CONFIG"
    PAYLOAD=$(cat "$LOCAL_CONFIG")
elif [ -f "$CONFIG_PATH" ]; then
    echo "Using user config: $CONFIG_PATH"
    PAYLOAD=$(cat "$CONFIG_PATH")
else
    echo "No local config found. Sending empty config for fresh setup."
    PAYLOAD="{}"
fi

# 3. Send setup request
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
