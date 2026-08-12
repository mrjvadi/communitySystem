# Community System

Community revenue distribution and fraud scoring in one repository, kept as independently deployable services.

## Services

- `community-service`: community accounting, validation, and earning event generation.
- `fraud-engine`: user/community quality scoring and fraud signals.

## Development

```bash
go test ./community-service/... ./fraud-engine/... ./shared/...
```

Build each image from the repository root:

```bash
docker build -f community-service/Dockerfile -t community-service .
docker build -f fraud-engine/Dockerfile -t fraud-engine .
```
