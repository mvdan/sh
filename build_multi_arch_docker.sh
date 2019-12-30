#!/bin/bash
# This script builds and deploys multi-architecture docker images.
#
# Can be run stand-alone from the command line like so:
#   DOCKER_USER=... DOCKER_PASSWORD=... DOCKER_BASE=... DOCKER_PLATFORMS=... \
#   TAGS=... \
#   (source ./build_multi_arch_docker.sh; build-multi-arch-docker::main)
# Where the environment variables are:
#   DOCKER_USER: Docker Hub user name to which to push images.
#   DOCKER_PASSWORD: Docker Hub password for above user.
#   DOCKER_BASE: Docker image base name (user/image).
#   TAGS: A set of image tags to which to deploy. Appended to DOCKER_BASE.
#   DOCKER_PLATFORMS: Docker buildx platforms (see `docker buildx ls`).
# Example:
#   DOCKER_USER=foo DOCKER_PASSWORD=bar DOCKER_BASE=foo/shfmt TAGS=latest \
#   (source ./build_multi_arch_docker.sh; build-multi-arch-docker::main)

function build_multi_arch_docker::install_docker_buildx() {
	# Install up-to-date version of docker, with buildx support.
	curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
	local -r os="$(lsb_release -cs)"
	sudo add-apt-repository \
		"deb [arch=amd64] https://download.docker.com/linux/ubuntu $os stable"
	sudo apt-get update
	sudo apt-get -y -o Dpkg::Options::="--force-confnew" install docker-ce

	# Enable docker daemon experimental support (for 'pull --platform').
	docker_daemone_config='/etc/docker/daemon.json'
	if [[ -e "$docker_daemone_config" ]]; then
		sudo sed -i -e 's/{/{ "experimental": true, /' "$docker_daemone_config"
	else
		sudo echo '{ "experimental": true }' | sudo tee "$docker_daemone_config"
	fi
	sudo systemctl restart docker

	# Install QEMU multi-architecture support for docker buildx.
	docker run --rm --privileged multiarch/qemu-user-static --reset -p yes

	# Instantiate docker buildx builder with multi-architecture support.
	export DOCKER_CLI_EXPERIMENTAL=enabled
	docker buildx create --name mybuilder
	docker buildx use mybuilder
	# Start up buildx and verify that all is OK.
	docker buildx inspect --bootstrap
}

# Log in to Docker Hub for deployment.
function build_multi_arch_docker::login_to_docker_hub() {
	echo "$DOCKER_PASSWORD" | docker login -u="$DOCKER_USERNAME" --password-stdin
}

# Run buildx build and push. Passed in arguments augment the command line.
function build_multi_arch_docker::buildx() {
	docker buildx build \
		--platform "$DOCKER_PLATFORMS" \
		--push \
		--progress plain \
		-f cmd/shfmt/Dockerfile \
		"$@" \
		.
}

# Build and push plain and busybox docker images for all tags.
function build_multi_arch_docker::build_and_push_all() {
	for tag in $TAGS; do
		build_multi_arch_docker::buildx -t "$DOCKER_BASE:$tag"
		build_multi_arch_docker::buildx -t "$DOCKER_BASE-busybox:$tag" \
			--target busybox
	done
}

# Test all pushed docker images on all platforms.
function build_multi_arch_docker::test_all() {
	printf '%s\n' "#!/bin/sh" "echo 'hello world'" >myscript

	for platform in ${DOCKER_PLATFORMS//,/ }; do
		for tag in $TAGS; do
			for ext in '' '-busybox'; do
				image="${DOCKER_BASE}${ext}:${tag}"
				echo "Testing docker image $image on platform $platform"
				docker pull --platform "$platform" "$image"
				if [ -n "$ext" ]; then
					docker run --entrypoint /bin/sh "$image" -c 'uname -m'
				fi
				docker run "$image" --version
				docker run -v "$PWD:/mnt" -w '/mnt' "$image" -d myscript
			done
		done
	done
}

function build_multi_arch_docker::main() {
	build_multi_arch_docker::install_docker_buildx
	build_multi_arch_docker::login_to_docker_hub
	build_multi_arch_docker::build_and_push_all
	build_multi_arch_docker::test_all
}
