#!/bin/bash

# Script to rebuild and restart the chat application docker containers
# Run this from the central-registry directory

set -e  # Exit on any error

echo "=========================================="
echo "  Chat Application Docker Restart Script"
echo "=========================================="
echo ""

# Check if docker-compose.yml exists
if [ ! -f "docker-compose.yml" ]; then
    echo "Error: docker-compose.yml not found in current directory"
    exit 1
fi

echo "🛑 Stopping and removing existing containers..."
docker compose down

echo ""
echo "🔨 Building and starting containers..."
docker compose up --build -d

echo ""
echo "✅ Containers started successfully!"
echo ""
echo "📋 Service URLs:"
echo "   - Registry API:  http://localhost:8080"
echo "   - Node A:        http://localhost:3001"
echo "   - Node B:        http://localhost:3002"
echo "   - Chat Client:   http://localhost:8081"
echo ""
echo "📝 To view logs: docker compose logs -f"
echo "🛑 To stop:      docker compose down"