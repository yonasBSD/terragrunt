# FROM oven/bun:1.2.2
# 
# WORKDIR /app
# COPY package.json bun.lock ./
# 
# RUN bun install
# 
# COPY . .
# 
# EXPOSE 4321
# 
# CMD ["bun", "run", "dev"]

# There's a bug that prevents us from using bun for this:
# https://github.com/oven-sh/bun/issues/17022

FROM node:22

WORKDIR /app
COPY package.json ./

RUN npm install

COPY . .

EXPOSE 4321

CMD ["npm", "run", "dev", "--", "--host", "0.0.0.0"]
