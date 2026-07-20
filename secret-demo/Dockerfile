FROM golang:1.26.5-bookworm

WORKDIR /src

COPY go.mod ./
RUN go mod tidy

COPY canary/ ./canary/
COPY secret_demo_test.go ./

ENV CANARY_HEX=53555045522d5345435245542d4b4559

CMD ["sh", "-c", "GOEXPERIMENT=runtimesecret go test -run TestSecretNotInCoreDump -v -count=1 -timeout 120s"]
