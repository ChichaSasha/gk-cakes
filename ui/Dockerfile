# UI artifact: SvelteKit adapter-node server. Built once from a verified
# main commit and deployed by digest. All npm packages are build-time only;
# adapter-node bundles what the server needs into build/.
FROM node:26.13.0-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94 AS build
WORKDIR /app
COPY package.json package-lock.json .npmrc ./
RUN npm ci
COPY . .
RUN npm run build

FROM node:26.13.0-alpine@sha256:c6ad9e9c1d80f6eb607c26b4b0a132ff7a2344fad1c78ea3baa2a7d4c8de6d94
LABEL org.opencontainers.image.source="https://github.com/ChichaSasha/gk-cakes" \
      org.opencontainers.image.description="GK Cakes customer and admin UI" \
      org.opencontainers.image.licenses="Apache-2.0"
WORKDIR /app
ENV NODE_ENV=production
COPY --from=build /app/package.json ./
COPY --from=build /app/build ./build
USER node
EXPOSE 3000
CMD ["node", "build"]
