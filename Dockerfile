# docker build --rm --target build -t ulean:alpha .
# docker run -dp 8001:50054 --name ubi ulean:alpha
#
# docker exec -it dbc44cef24b8 /bin/bash
#
# docker image ls
# docker ps
#
# docker logs ubi2 --follow
# docker logs ubi2
#
# docker kill <container name>
# docker rm <container name>
# docker rmi <image name>
# docker container prune -f
# docker image prune -af
# docker stop `docker ps -a -q `
#
FROM golang:1.21 AS build
WORKDIR /app
COPY . .
RUN go mod download
RUN go build -o raft raft.go
EXPOSE 7000
CMD [ "/app/raft" ]

FROM debian:bookworm-slim as lean
WORKDIR /
COPY --from=build /app/raft /raft
COPY --from=build /app/compose.yaml /compose.yaml
CMD [ "/raft"]
#CMD [ "/raft", "-d"]
