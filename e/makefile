SERVICE_NAME = entrenador-cdp
PROTO_FILE = proto\interaccion.proto
DOCKER_IMAGE = $(SERVICE_NAME)-image
DOCKER_CONTAINER = $(SERVICE_NAME)-container
PORT = 50051

docker-$(SERVICE_NAME): build docker-build docker-run

build:
	go build -o $(SERVICE_NAME) .

docker-build:
	docker build -t $(DOCKER_IMAGE) .

docker-run:
	docker run -it -p $(PORT):$(PORT) --name $(DOCKER_CONTAINER) $(DOCKER_IMAGE)
