# Builds one Go service: docker build -f deploy/backend.Dockerfile --build-arg SVC=user .   (or SVC=admin)
FROM golang:1.27-alpine AS build
ARG SVC
WORKDIR /src/backend
COPY backend/go.work ./
COPY backend/admin/go.mod backend/admin/go.sum admin/
COPY backend/user/go.mod backend/user/go.sum user/
RUN go mod download
COPY backend/ ./
RUN test -n "$SVC" && CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/app ./$SVC/cmd/$SVC \
 && mkdir -p /out/uploads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
# Owned by nonroot so a fresh named volume mounted here is writable.
COPY --from=build --chown=nonroot:nonroot /out/uploads /data/uploads
ENV UPLOAD_DIR=/data/uploads
VOLUME /data/uploads
USER nonroot
EXPOSE 8080 8090
ENTRYPOINT ["/app"]
