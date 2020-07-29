FROM golang:1.14.2-alpine as build
RUN apk update && apk add make git gcc libc-dev

# Pull dependencies first to leverage docker layer caching.
COPY go.mod /build/go.mod
COPY go.sum /build/go.sum
RUN cd /build && go mod download

# Now copy the rest of the sources to build the programs.
COPY . /build
RUN cd /build && go build ./cmd/frontend

FROM alpine
RUN apk update && apk add ca-certificates curl bind-tools

COPY --from=build /build/frontend /usr/local/bin/frontend
COPY ./content/static/ /var/lib/pkgsite/content/static/

EXPOSE 8080
# The entrypoint is missing the value of the -proxy_url argument.
# This is intentional so users can use the docker image by running it as
#
#    docker run pkgsite <proxy-url>
#
ENTRYPOINT ["frontend", "-http", "8080", "-static", "/var/lib/pkgsite/content/static", "-direct_proxy", "-proxy_url"]
