FROM golang:1.14.2 as build
RUN apt-get update && apt-get install -y build-essential git libc-dev

# Pull dependencies first to leverage docker layer caching.
COPY go.mod /build/go.mod
COPY go.sum /build/go.sum
RUN cd /build && go mod download

# Now copy the rest of the sources to build the programs.
COPY . /build
RUN cd /build && go build ./cmd/frontend

FROM ubuntu
RUN apt-get update && apt-get install -y ca-certificates curl gnupg lsb-core
# golang-migrate/migrate is used to mange migrations, probably useful to have
# it on the image.
#
# https://github.com/golang-migrate/migrate/releases
RUN curl -L https://packagecloud.io/golang-migrate/migrate/gpgkey | apt-key add -
RUN echo "$(lsb_release -sc)"
RUN echo "deb https://packagecloud.io/golang-migrate/migrate/ubuntu/ $(lsb_release -sc) main" > /etc/apt/sources.list.d/migrate.list
RUN apt-get update && apt-get install -y migrate

COPY ./content/static/ /var/lib/pkgsite/content/static/
COPY ./devtools/ /var/lib/pkgsite/devtools/
COPY ./migrations /var/lib/pkgsite/migrations/
COPY ./third_party /var/lib/pkgsite/third_party/
COPY --from=build /build/frontend /usr/local/bin/frontend

ENV GO_DISCOVERY_DATABASE_USER=postgres \
    GO_DISCOVERY_DATABASE_PASSWORD='' \
    GO_DISCOVERY_DATABASE_HOST=postgres \
    GO_DISCOVERY_DATABASE_NAME=discovery-db

EXPOSE 8080
# The entrypoint is missing the value of the -proxy_url argument.
# This is intentional so users can use the docker image by running it as
#
#    docker run pkgsite <proxy-url>
#
ENTRYPOINT ["frontend", "-http", ":8080", "-static", "/var/lib/pkgsite/content/static", "-third_party", "/var/lib/pkgsite/third_party", "-direct_proxy", "-proxy_url"]
