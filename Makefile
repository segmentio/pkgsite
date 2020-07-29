branch = $(shell git rev-parse --abbrev-ref HEAD)
commit = $(shell git rev-parse --short=7 HEAD)

docker.version ?= "$(branch)-$(commit)"
docker.image ?= "$(DOCKER_REPOSITORY)/pkgsite:$(docker.version)"
docker:
	docker build -f Dockerfile -t $(docker.image) .

publish: docker
	docker push $(docker.image)

.PHONY: docker publish
