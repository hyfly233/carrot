## applicationmaster

```bash
protoc --go_out=applicationmaster --go_opt=paths=source_relative \
    --go-grpc_out=applicationmaster --go-grpc_opt=paths=source_relative \
    applicationmaster.proto
```

## containermanager

```bash
protoc --go_out=containermanager --go_opt=paths=source_relative \
    --go-grpc_out=containermanager --go-grpc_opt=paths=source_relative \
    containermanager.proto
```

## nodemanager

```bash
protoc --go_out=nodemanager --go_opt=paths=source_relative \
    --go-grpc_out=nodemanager --go-grpc_opt=paths=source_relative \
    nodemanager.proto
```

## resourcemanager

```bash
protoc --go_out=resourcemanager --go_opt=paths=source_relative \
    --go-grpc_out=resourcemanager --go-grpc_opt=paths=source_relative \
    resourcemanager.proto
```
