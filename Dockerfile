FROM gcr.io/distroless/static-debian13:nonroot-amd64@sha256:d4df213e27178b79e05ee2bdf8a23d8c54baab3b5a0dc083b039ad9929fa4219
ARG SOURCE_SHA
ARG VERSION
LABEL org.opencontainers.image.title="Exta" \
  org.opencontainers.image.source="https://github.com/QianFuv/Exta" \
  org.opencontainers.image.revision="${SOURCE_SHA}" \
  org.opencontainers.image.version="${VERSION}"
COPY --chmod=0555 exta-linux-amd64 /exta
COPY --chmod=0444 dependency-inventory.json frontend-provenance.json /usr/share/exta/
COPY --chmod=0555 licenses/ /usr/share/exta/licenses/
COPY --chmod=0555 frontend-licenses/ /usr/share/exta/frontend-licenses/
ENV HOME=/var/lib/exta
USER 65532:65532
EXPOSE 8789
ENTRYPOINT ["/exta"]
CMD ["serve", "--data-dir", "/var/lib/exta/state"]
