FROM golang:1.19.3-alpine3.16
WORKDIR /app

COPY go.mod ./
COPY go.sum ./
COPY ./pkg ./pkg
COPY main.go ./

# When debugging by replacing the lib-volume-populator with a local modified copy
# COPY . ./

RUN go mod download
RUN go build -o /main

RUN apk add gcc py3-pip python3-dev linux-headers libc-dev libxml2-dev curl-dev qemu-img
RUN pip install ovirt-engine-sdk-python ovirt-imageio

ENTRYPOINT ["/main"]
