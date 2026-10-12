FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GIT_SHA=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/yaqyn/waymark-link-shortener/internal/build.GitSHA=${GIT_SHA} -X github.com/yaqyn/waymark-link-shortener/internal/build.BuildTime=${BUILD_TIME}" -o /waymark .

FROM alpine:3.23
RUN addgroup -g 10001 waymark && adduser -D -u 10001 -G waymark waymark && mkdir /data && chown waymark:waymark /data
COPY --from=build /waymark /usr/local/bin/waymark
ENV WAYMARK_BIND_ADDR=0.0.0.0
USER 10001:10001
WORKDIR /data
EXPOSE 8899
ENTRYPOINT ["waymark"]
CMD ["-data", "/data"]
