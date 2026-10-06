# Деплой DURAG в продакшен

Пошаговая инструкция «с нуля до работающей Mini App». Всё нужное лежит в репозитории:

| Файл | Назначение |
| --- | --- |
| `docker-compose.prod.yml` | Nginx (TLS, прокси `/ws`, статика), Certbot, Go-бэкенд, Redis |
| `deploy/server-setup.sh` | Разовая подготовка VPS: Docker + compose plugin, ufw (22/80/443) |
| `deploy/init-letsencrypt.sh` | Первый сертификат Let's Encrypt (dummy-сертификат → nginx → webroot-челлендж) |
| `deploy/deploy.sh` | Сборка образов, запуск/обновление стека, health-check через HTTPS |
| `deploy/nginx/templates/default.conf.template` | Конфиг Nginx (подставляется `${DOMAIN}`) |
| `deploy/nginx/entrypoint.d/90-reload-loop.sh` | Перечитывание сертификатов Nginx раз в 6 ч |
| `.github/workflows/ci.yml` | CI: `go test -race`, vitest, `vite build`, сборка Docker-образов |
| `.github/workflows/deploy.yml` | CD: по пушу в `main` или вручную — `ssh` на сервер и `deploy/deploy.sh` |
| `Makefile` | Короткие команды: `make test`, `make cert`, `make deploy`, `make prod-logs` |
| `deploy/remote.py` | Деплой по SSH с локальной машины или из песочницы агента: `check` / `bootstrap` / `update` / `status` / `logs` |
| `deploy/AGENT_HANDOFF.md` | Инструкция для ИИ-агента-деплоера (переменные окружения, порядок, проверки, запреты) |

> Деплоит ИИ-агент с доступом через переменные окружения? Ему достаточно [deploy/AGENT_HANDOFF.md](deploy/AGENT_HANDOFF.md):
> `pip install paramiko && python3 deploy/remote.py check && python3 deploy/remote.py bootstrap`.

## Чек-лист

- [ ] VPS с публичным IP (1 vCPU / 1 ГБ RAM достаточно), Ubuntu 22.04/24.04 или Debian 12
- [ ] Домен, A-запись (и AAAA, если есть IPv6) указывает на сервер — **до** выпуска сертификата
- [ ] Открыты порты 80 и 443
- [ ] Токен бота от [@BotFather](https://t.me/BotFather) (`/newbot`)
- [ ] Короткое имя Mini App (`/newapp`) — нужно для deep-link'ов на стол

## 1. Подготовка сервера (один раз)

```bash
ssh root@SERVER_IP
apt-get update && apt-get install -y git
git clone https://github.com/BreezeAreaSay/durag.git /opt/durag
cd /opt/durag
./deploy/server-setup.sh        # Docker Engine, compose plugin, ufw: OpenSSH/80/443
```

Если деплоить будет отдельный пользователь (рекомендуется для CD): `adduser deploy && usermod -aG docker deploy`,
`chown -R deploy:deploy /opt/durag`.

## 2. Конфигурация `.env`

```bash
cp .env.example .env
nano .env
```

Обязательные значения для продакшена:

```dotenv
TELEGRAM_BOT_TOKEN=123456789:AA...      # токен от @BotFather
ALLOW_DEV_AUTH=false                    # вход только через Telegram (compose и так принудительно ставит false)
DOMAIN=durag.example.com                # ваш домен
CERTBOT_EMAIL=you@example.com           # уведомления Let's Encrypt
VITE_BOT_USERNAME=MyDuragBot            # username бота без @
VITE_APP_SHORTNAME=durag                # короткое имя Mini App из /newapp
ROOM_TTL=24h
TG_INITDATA_MAX_AGE=24h
BOUT_RESOLVE_DELAY=3500ms               # пауза после кона, чтобы все увидели отбой
STUMP_AUTO_DELAY=4s                     # сколько игрок может сам взять пенёк после кона, потом берёт сервер
TURN_TIMEOUT=45s                        # таймер хода (0 — выключить)
```

`PORT`, `REDIS_ADDR`, `ALLOWED_ORIGINS` в проде задаёт `docker-compose.prod.yml` (`ALLOWED_ORIGINS=https://$DOMAIN`).
`VITE_*` вшиваются во фронтенд при сборке: после их изменения нужен повторный `./deploy/deploy.sh`.

## 3. Сертификат Let's Encrypt (один раз)

```bash
./deploy/init-letsencrypt.sh              # прод-сертификат
# STAGING=1 ./deploy/init-letsencrypt.sh  # тестовый CA, без лимитов — для проверки DNS/портов
```

Скрипт: создаёт временный самоподписанный сертификат → поднимает nginx (и бэкенд/фронтенд, т.к. nginx зависит от них) →
удаляет временный → запрашивает настоящий через webroot `/.well-known/acme-challenge/` → `nginx -s reload`.
Сертификаты хранятся в `deploy/certbot/conf` (не в git). Продление: контейнер `certbot` запускает `certbot renew`
каждые 12 ч, nginx перечитывает файлы каждые 6 ч.

## 4. Запуск

```bash
./deploy/deploy.sh
# эквивалент: docker compose -f docker-compose.prod.yml up -d --build
```

Скрипт собирает образы, поднимает стек и ждёт `https://$DOMAIN/healthz`. Проверка руками:

```bash
curl https://durag.example.com/healthz          # ok
curl https://durag.example.com/api/config       # {"dev_auth":false,"max_players":6,"min_players":2}
docker compose -f docker-compose.prod.yml ps    # все сервисы Up, backend (healthy)
```

В обычном браузере `https://durag.example.com` покажет «Игра открывается только из Telegram» и кнопку «Открыть в Telegram» —
это ожидаемо: без подписи `initData` сервер никого не пускает.

## 5. Настройка бота в @BotFather

1. `/newapp` → выбрать бота → название → описание → картинка 640×360 (GIF можно пропустить) →
   **Web App URL:** `https://durag.example.com/` → короткое имя (например `durag` — оно же `VITE_APP_SHORTNAME`).
2. `/setmenubutton` → бот → URL `https://durag.example.com/` → текст кнопки, например «Играть».
3. Ссылки:
   * открыть игру — `https://t.me/MyDuragBot/durag`
   * позвать за конкретный стол — `https://t.me/MyDuragBot/durag?startapp=КОД` (её же генерирует кнопка «Копировать» в комнате ожидания).

Telegram открывает Mini App только по HTTPS с валидным сертификатом, поэтому шаг 3 обязателен.

## 6. Обновление и откат

```bash
cd /opt/durag
git pull && ./deploy/deploy.sh      # или ./deploy/deploy.sh --pull
```

Откат: `git checkout <коммит> && ./deploy/deploy.sh`. Состояние столов живёт в Redis (TTL 24 ч) и не зависит от
версии кода, но при несовместимом изменении `GameState` его можно сбросить: `docker compose -f docker-compose.prod.yml exec redis redis-cli FLUSHALL`.

## 7. CI/CD через GitHub Actions

`ci.yml` запускается на каждый push и PR: Go-тесты с `-race`, vitest, `vite build`, сборка обоих Docker-образов.

`deploy.yml` выкатывает по пушу в `main` (или вручную через *Run workflow*). В **Settings → Secrets and variables → Actions** добавьте:

| Секрет | Значение |
| --- | --- |
| `DEPLOY_HOST` | IP или домен сервера |
| `DEPLOY_USER` | пользователь с доступом к docker (например `deploy`) |
| `DEPLOY_SSH_KEY` | приватный ключ (`ssh-keygen -t ed25519 -C durag-deploy`; публичную часть — в `~/.ssh/authorized_keys` на сервере) |
| `DEPLOY_PORT` | (необязательно) порт SSH, по умолчанию 22 |
| `DEPLOY_PATH` | (необязательно) путь к клону, по умолчанию `/opt/durag` |

Workflow делает `git checkout <ветка push'а> && git pull --ff-only && ./deploy/deploy.sh`.
Если деплоите не из `main`, поправьте `branches` в `deploy.yml`.

## 8. Эксплуатация

```bash
docker compose -f docker-compose.prod.yml logs -f --tail=100 backend   # логи (JSON, slog)
docker compose -f docker-compose.prod.yml logs -f nginx
docker compose -f docker-compose.prod.yml run --rm certbot certificates # срок действия сертификата
docker compose -f docker-compose.prod.yml up -d --scale backend=2       # несколько инстансов бэкенда
docker compose -f docker-compose.prod.yml down                          # остановить (данные Redis сохраняются в volume)
```

* Масштабирование: инстансы бэкенда синхронизируют комнаты через Redis Pub/Sub; nginx резолвит `backend` через DNS
  Docker на каждый запрос, поэтому `--scale` работает без правок конфига.
* Бэкап: достаточно `.env` и `deploy/certbot/conf`. Состояние столов эфемерно.
* Мониторинг: `GET /healthz` (200 `ok`); контейнер `backend` имеет Docker HEALTHCHECK.
* Ресурсы: стек целиком — ~150 МБ RAM в простое.

## 9. Диагностика

| Симптом | Причина / что сделать |
| --- | --- |
| `init-letsencrypt.sh` падает на запросе сертификата | DNS ещё не указывает на сервер; закрыт порт 80; лимиты LE (5 выпусков/неделю на домен) — проверьте со `STAGING=1` |
| Nginx в рестарт-цикле | Нет файлов сертификата по пути `deploy/certbot/conf/live/$DOMAIN/` — выполните шаг 3 |
| Mini App открывается, но «Переподключение…» | WebSocket не проходит: `curl -i -N -H "Connection: Upgrade" -H "Upgrade: websocket" -H "Sec-WebSocket-Version: 13" -H "Sec-WebSocket-Key: dGVzdA==" https://$DOMAIN/ws` должен вернуть `101`; проверьте `ALLOWED_ORIGINS` (должен быть `https://$DOMAIN`) и логи backend |
| «Не удалось войти» внутри Telegram | Неверный `TELEGRAM_BOT_TOKEN` (бот из `/newapp` должен совпадать с токеном), либо часы сервера врут (`timedatectl`; `auth_date` старше `TG_INITDATA_MAX_AGE`) |
| «Игра открывается только из Telegram» внутри Telegram | Приложение открыто по прямой ссылке в браузере, а не через кнопку/`t.me/<bot>/<app>` — `initData` пустой |
| Telegram показывает старую версию после деплоя | Закройте и заново откройте Mini App; статика кэшируется с `Cache-Control: no-cache` для `index.html`, ассеты — по хэшу |
| `docker compose` ругается на `.env` | Значения с пробелами берите в кавычки; не оставляйте `REDIS_ADDR=` пустым — в проде его задаёт compose |

## 10. Тестирование TMA без сервера (ngrok)

```bash
docker compose up                    # dev-стек на :5173
ngrok http 5173                      # https://xxxx.ngrok-free.app
```

Вставьте HTTPS-URL ngrok в `/newapp` (или временно в `/setmenubutton`). Dev-сервер Vite проксирует `/ws` и `/api`,
так что одного туннеля достаточно. Для проверки настоящей подписи поставьте в `.env` `ALLOW_DEV_AUTH=false` и реальный токен.
