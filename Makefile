# Convenience targets. See README.md and DEPLOY.md.
PROD := docker compose -f docker-compose.prod.yml

.PHONY: dev test lint build prod-up prod-down prod-logs prod-ps cert deploy

dev:            ## dev stack: redis + go run + vite (http://localhost:5173)
	docker compose up

test:           ## backend + frontend tests
	cd backend && go test -race ./...
	cd frontend && npm test

lint:           ## gofmt/vet + typecheck
	cd backend && test -z "$$(gofmt -l .)" && go vet ./...
	cd frontend && npm run typecheck

build:          ## production frontend bundle + backend binary
	cd frontend && npm run build
	cd backend && go build -o bin/server ./cmd/server

cert:           ## first Let's Encrypt certificate (needs DOMAIN/CERTBOT_EMAIL in .env)
	./deploy/init-letsencrypt.sh

deploy:         ## build + start + health check (also the update procedure)
	./deploy/deploy.sh

prod-up:
	$(PROD) up -d --build

prod-down:
	$(PROD) down

prod-logs:
	$(PROD) logs -f --tail=100

prod-ps:
	$(PROD) ps
