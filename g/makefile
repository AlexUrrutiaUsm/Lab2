SERVICE_NAME = gimnasio
PROTO_FILE = proto\interaccion.proto
DOCKER_IMAGE = $(SERVICE_NAME)-image
DOCKER_CONTAINER = $(SERVICE_NAME)-container
PORT = 50053

docker-$(SERVICE_NAME): build docker-build docker-run

build:
	go build -o $(SERVICE_NAME) .

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	-@docker rm -f $(DOCKER_CONTAINER) 2>NUL || exit 0
	docker run -it -p $(PORT):$(PORT) --name $(DOCKER_CONTAINER) $(DOCKER_IMAGE)