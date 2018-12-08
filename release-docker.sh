#!/bin/bash

tag=$(git describe --exact-match 2>/dev/null)
if [[ $? != 0 ]]; then
	tag=latest
fi

docker build -t mvdan/shfmt:$tag -f cmd/shfmt/Dockerfile .

if [[ -n $DOCKER_PASSWORD ]]; then
	echo "$DOCKER_PASSWORD" | docker login -u "$DOCKER_USERNAME" --password-stdin
fi

docker push mvdan/shfmt:$tag
