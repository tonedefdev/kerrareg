FROM node:22-alpine

WORKDIR /app

COPY package*.json ./
RUN npm ci

COPY . .
RUN npm run build

EXPOSE 8081

ENV PORT=8081
ENV KERRAREG_K8S_AUTH_MODE=incluster
CMD ["npm", "run", "start"]
