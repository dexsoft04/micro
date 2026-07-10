#!/bin/bash

set -e

if [ ! "$IMAGE" ]; then
  IMAGE=micro/cells
fi

if [ "$CELL_TAG" ] && [ ! "$CELL_DIR" ]; then
  echo "CELL_TAG requires CELL_DIR"
  exit 1
fi

if [ "$PUSH" != "false" ]; then
  echo ${PASSWORD} | docker login $DOCKER_DOMAIN -u ${USERNAME} --password-stdin
fi

build_cell() {
  dir=$1

  if [ ! -d "${dir}" ]; then
    return
  fi
  TAGPREFIX=
  if [ "$DOCKER_DOMAIN" ]; then
    TAGPREFIX=$DOCKER_DOMAIN/
  fi
  tag_name=${CELL_TAG:-$dir}
  TAG=$TAGPREFIX$IMAGE:$tag_name

  pushd "${dir}" &>/dev/null
  echo Building $TAG

  if [ ! -s Dockerfile ]; then
    echo Skipping $TAG
    popd &>/dev/null
    return
  fi

  if [ "$PUSH" = "false" ]; then
    docker build ${DOCKER_BUILD_FLAGS:-} -t $TAG .
  else
    docker buildx build --platform linux/amd64 --platform linux/arm64 --tag $TAG --push .
  fi

  popd &>/dev/null
}

if [ "$CELL_DIR" ]; then
  build_cell "$CELL_DIR"
  exit 0
fi

ls | while read dir; do
  build_cell "$dir"
done
