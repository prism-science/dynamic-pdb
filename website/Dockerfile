# syntax=docker/dockerfile:1
FROM harbor.astera.sh/dockerhub-cache/node:22-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# @dynamic-pdb/hetkit and @dynamic-pdb/hetstar are `file:` deps pointing into the
# vendored submodule, so npm has to read their manifests to resolve the lock. Only
# the manifests are copied here: their source arrives with `COPY . .` in the builder
# stage, and this keeps the install layer cached across ordinary hetstar edits.
COPY vendor/hetstar/packages/hetkit/package.json ./vendor/hetstar/packages/hetkit/
COPY vendor/hetstar/packages/hetstar/package.json ./vendor/hetstar/packages/hetstar/
RUN npm ci

FROM harbor.astera.sh/dockerhub-cache/node:22-alpine AS builder
WORKDIR /app
ARG NEXT_PUBLIC_API_BASE_URL=https://dynamicpdb.com/api
ENV NEXT_PUBLIC_API_BASE_URL=$NEXT_PUBLIC_API_BASE_URL
ENV NEXT_TELEMETRY_DISABLED=1
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

FROM harbor.astera.sh/dockerhub-cache/node:22-alpine
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0
RUN addgroup -S nodejs && adduser -S nextjs -G nodejs
COPY --from=builder --chown=nextjs:nodejs /app/.next ./.next
COPY --from=builder --chown=nextjs:nodejs /app/node_modules ./node_modules
COPY --from=builder --chown=nextjs:nodejs /app/package.json ./package.json
COPY --from=builder --chown=nextjs:nodejs /app/public ./public
USER nextjs
EXPOSE 3000
CMD ["npm", "run", "start"]
