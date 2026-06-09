#!/bin/bash
# Build and push Docker image to private registry
# Usage: ./build-and-push.sh [registry] [image-name] [tag]

set -e

# Configuration
REGISTRY="${1:-46.38.157.108:80}"
IMAGE_NAME="${2:-sub2api}"

# Get version from VERSION file if not specified
if [ -z "$3" ]; then
  VERSION_FILE="backend/cmd/server/VERSION"
  if [ -f "$VERSION_FILE" ]; then
    VERSION=$(tr -d '\r\n' < "$VERSION_FILE")
    COMMIT=$(git rev-parse --short HEAD)
    TAG="v${VERSION}-${COMMIT}"
  else
    TAG=$(git rev-parse --short HEAD)
  fi
else
  TAG="$3"
fi

LATEST_TAG="latest"

echo "=========================================="
echo "Sub2API Docker Build & Push"
echo "=========================================="
echo "Registry:   $REGISTRY"
echo "Image:      $IMAGE_NAME"
echo "Tag:        $TAG"
echo "Latest Tag: $LATEST_TAG"
echo "=========================================="
echo ""

# Verify git branch
CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD)
echo "Current branch: $CURRENT_BRANCH"
if [ "$CURRENT_BRANCH" != "claude/request-body-transform-7i2fjw" ]; then
  echo "WARNING: Not on claude/request-body-transform-7i2fjw branch!"
  read -p "Continue anyway? (y/n) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    exit 1
  fi
fi

# Build the image
echo "Building Docker image: $REGISTRY/$IMAGE_NAME:$TAG"
docker build \
  -t "$REGISTRY/$IMAGE_NAME:$TAG" \
  -t "$REGISTRY/$IMAGE_NAME:$LATEST_TAG" \
  .

if [ $? -ne 0 ]; then
  echo "ERROR: Docker build failed"
  exit 1
fi

echo ""
echo "Build completed successfully!"
echo ""

# Push to registry
echo "Pushing images to registry..."
echo "  - $REGISTRY/$IMAGE_NAME:$TAG"
echo "  - $REGISTRY/$IMAGE_NAME:$LATEST_TAG"
echo ""

docker push "$REGISTRY/$IMAGE_NAME:$TAG"
if [ $? -ne 0 ]; then
  echo "ERROR: Failed to push tag version"
  exit 1
fi

docker push "$REGISTRY/$IMAGE_NAME:$LATEST_TAG"
if [ $? -ne 0 ]; then
  echo "ERROR: Failed to push latest version"
  exit 1
fi

echo ""
echo "=========================================="
echo "✓ Push completed successfully!"
echo "=========================================="
echo "Images pushed:"
echo "  - $REGISTRY/$IMAGE_NAME:$TAG"
echo "  - $REGISTRY/$IMAGE_NAME:$LATEST_TAG"
echo ""
echo "You can now deploy with:"
echo "  docker pull $REGISTRY/$IMAGE_NAME:$TAG"
echo ""
