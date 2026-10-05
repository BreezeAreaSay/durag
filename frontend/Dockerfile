# ---- build stage -----------------------------------------------------------
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
ARG VITE_BOT_USERNAME=
ARG VITE_APP_SHORTNAME=
ENV VITE_BOT_USERNAME=$VITE_BOT_USERNAME VITE_APP_SHORTNAME=$VITE_APP_SHORTNAME
RUN npm run build

# ---- runtime stage: static files served by nginx (see deploy/nginx) --------
FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
