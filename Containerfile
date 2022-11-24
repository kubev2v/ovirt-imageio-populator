FROM golang:1.19.3-alpine3.16
WORKDIR /app

COPY go.mod ./
COPY go.sum ./
COPY *.go ./

# When debugging by replacing the lib-volume-populator with a local modified copy
# COPY . ./

RUN go mod download
RUN go build -o /main

RUN apk add gcc py3-pip python3-dev linux-headers libc-dev libxml2-dev curl-dev qemu-img git
RUN pip install ovirt-engine-sdk-python
RUN pip install git+https://github.com/bennyz/ovirt-imageio.git@block-dev-progress

ENTRYPOINT ["/main"]
