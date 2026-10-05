# Инструкция для агента-деплоера DURAG

Этот документ рассчитан на ИИ-агента (Claude Code / Cursor / любой), которому выдали доступ к серверу через
переменные окружения. Человек не передаёт пароли в чат: всё берётся из env. Полная документация по стеку —
[DEPLOY.md](../DEPLOY.md), архитектура и правила — [README.md](../README.md).

## 0. Что ты деплоишь

* Репозиторий: `https://github.com/BreezeAreaSay/durag`, ветка `claude/ecstatic-maxwell-h3azwt`.
* Прод-стек: `docker-compose.prod.yml` — Nginx (TLS, WebSocket-прокси, статика), Certbot, Go-бэкенд, Redis.
* Всё необходимое уже в репозитории: `deploy/server-setup.sh`, `deploy/init-letsencrypt.sh`, `deploy/deploy.sh`
  и драйвер `deploy/remote.py`, который выполняет эти шаги по SSH.

## 1. Переменные окружения, которые тебе должны выдать

| Переменная | Обязательно | Назначение |
| --- | --- | --- |
| `DEPLOY_HOST` | да | IP или хост сервера |
| `DEPLOY_USER` | нет (root) | SSH-пользователь; не root — нужен sudo |
| `DEPLOY_PORT` | нет (22) | порт SSH |
| `DEPLOY_PASSWORD` **или** `DEPLOY_SSH_KEY` / `DEPLOY_SSH_KEY_PATH` | да | пароль либо приватный ключ (текст или путь) |
| `DOMAIN` | да | домен, A-запись которого указывает на сервер |
| `CERTBOT_EMAIL` | да | почта для Let's Encrypt |
| `TELEGRAM_BOT_TOKEN` | да | токен бота от @BotFather |
| `VITE_BOT_USERNAME` | желательно | username бота без `@` |
| `VITE_APP_SHORTNAME` | желательно | короткое имя Mini App из `/newapp` |
| `DEPLOY_PATH`, `DEPLOY_BRANCH`, `DEPLOY_REPO` | нет | переопределения пути `/opt/durag`, ветки и репозитория |
| `LE_STAGING=1` | нет | тестовый CA Let's Encrypt (без лимитов), только для проверки DNS/портов |

Если чего-то из обязательного нет — остановись и запроси у человека, ничего не выдумывай.

## 2. Порядок действий

```bash
pip install paramiko                      # единственная зависимость драйвера
python3 deploy/remote.py check            # SSH, docker, публичный IP сервера и куда смотрит DNS
python3 deploy/remote.py bootstrap        # полный первый деплой; идемпотентен, можно перезапускать
```

`bootstrap` делает по шагам: git clone/pull в `DEPLOY_PATH` → `server-setup.sh` (Docker, compose, ufw 22/80/443) →
записывает `.env` на сервер из твоих переменных (секреты в вывод не печатаются) → ждёт, пока `DOMAIN` начнёт резолвиться
в IP сервера → `init-letsencrypt.sh` (если сертификата ещё нет) → `deploy.sh` (сборка, запуск, ожидание `/healthz`) →
проверяет `https://DOMAIN/healthz`, `/api/config` и `/` снаружи.

Успех выглядит так:

```
GET https://DOMAIN/healthz -> 200 ok
GET https://DOMAIN/api/config -> 200 {"dev_auth":false,"max_players":6,"min_players":2}
GET https://DOMAIN/ -> 200
```

Дополнительно проверь WebSocket-рукопожатие (ожидается `101 Switching Protocols`):

```bash
curl -s -o /dev/null -w '%{http_code}\n' -N \
  -H 'Connection: Upgrade' -H 'Upgrade: websocket' -H 'Sec-WebSocket-Version: 13' \
  -H 'Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==' "https://$DOMAIN/ws"
```

## 3. Что сообщить человеку после деплоя

1. Адрес: `https://DOMAIN/` (в браузере покажет «Игра открывается только из Telegram» — это норма).
2. Что надо сделать в @BotFather руками: `/newapp` → выбрать бота → Web App URL `https://DOMAIN/` → короткое имя,
   совпадающее с `VITE_APP_SHORTNAME`; затем `/setmenubutton` с тем же URL. Ссылка на игру:
   `https://t.me/<VITE_BOT_USERNAME>/<VITE_APP_SHORTNAME>`, на стол: `...?startapp=КОД`.
3. Как обновлять: `python3 deploy/remote.py update` (или на сервере `cd /opt/durag && git pull && ./deploy/deploy.sh`).
4. Логи: `python3 deploy/remote.py logs backend` / `logs nginx`; статус: `python3 deploy/remote.py status`.

## 4. Правила безопасности

* Не печатай значения `TELEGRAM_BOT_TOKEN`, `DEPLOY_PASSWORD`, ключей; не коммить `.env`.
* Не выполняй `docker compose down -v` (удалит volume Redis), не трогай `deploy/certbot/conf` и DNS.
* `redis-cli FLUSHALL` (сброс всех столов) — только по явной просьбе человека.
* Если `init-letsencrypt.sh` падает: сначала `LE_STAGING=1 python3 deploy/remote.py bootstrap` для проверки DNS и портов,
  потом без staging. Лимит Let's Encrypt — 5 выпусков в неделю на домен.
* Не меняй код проекта ради деплоя. Если что-то не собирается — сообщи вывод человеку.

## 5. Если `remote.py` недоступен

Те же шаги руками по SSH (`sshpass -p "$DEPLOY_PASSWORD" ssh -o StrictHostKeyChecking=accept-new $DEPLOY_USER@$DEPLOY_HOST`):

```bash
apt-get update && apt-get install -y git
git clone -b claude/ecstatic-maxwell-h3azwt https://github.com/BreezeAreaSay/durag.git /opt/durag
cd /opt/durag && ./deploy/server-setup.sh
cp .env.example .env && nano .env         # TELEGRAM_BOT_TOKEN, DOMAIN, CERTBOT_EMAIL, VITE_*, ALLOW_DEV_AUTH=false
./deploy/init-letsencrypt.sh
./deploy/deploy.sh
```

## 6. Диагностика

| Симптом | Что делать |
| --- | --- |
| `bootstrap` остановился на DNS | A-запись `DOMAIN` ещё не указывает на IP сервера; попроси человека исправить, перезапусти |
| `init-letsencrypt.sh` упал | закрыт порт 80 (`ufw status`), DNS не распространился или лимит LE → `LE_STAGING=1` для проверки |
| Nginx в рестарт-цикле | нет файлов сертификата в `deploy/certbot/conf/live/DOMAIN/` → повторить шаг сертификата |
| `/healthz` недоступен, но контейнеры Up | `python3 deploy/remote.py logs nginx`, затем `logs backend`; проверь, что `.env` содержит `DOMAIN` |
| WebSocket не даёт 101 | проверь `ALLOWED_ORIGINS=https://DOMAIN` в `.env` и логи backend |
| В Telegram «Не удалось войти» | неверный `TELEGRAM_BOT_TOKEN` (бот из `/newapp` должен совпадать с токеном), часы сервера (`timedatectl`) |
