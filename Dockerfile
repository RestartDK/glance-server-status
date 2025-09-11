FROM golang:1.24-alpine

# Install runtime dependencies needed for system monitoring
RUN apk add --no-cache \
    lm-sensors \
    coreutils \
    procps

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

EXPOSE 8080
CMD ["./main"]