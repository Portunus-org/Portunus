import type { Locale } from "@/lib/messages"

/**
 * The built-in documentation. Content lives here, in both interface
 * languages, so it ships inside the panel and matches the installed version.
 *
 * Inline markup in strings: **bold**, `code`, [link](/path/).
 */
export type DocBlock =
  | { h: string }
  | { p: string }
  | { ul: string[] }
  | { ol: string[] }
  | { code: string }
  | { note: string }

export interface DocTopic {
  slug: string
  title: Record<Locale, string>
  summary: Record<Locale, string>
  body: Record<Locale, DocBlock[]>
}

export const docs: DocTopic[] = [
  {
    slug: "start",
    title: { en: "Getting started", ru: "Быстрый старт" },
    summary: {
      en: "From an empty panel to a working connection in three steps.",
      ru: "От пустой панели до работающего подключения за три шага.",
    },
    body: {
      en: [
        { p: "Portunus manages a sing-box proxy server. You create connections (the ports and protocols the server listens on), add clients (the people or devices that connect), and hand each client a link or a QR code." },
        { h: "1. Create a connection" },
        { p: "Open [Connections](/inbounds/) and press **Add connection**. The wizard checks the server and suggests the settings: a free port, a site to imitate, a certificate. Press **Next** a few times and then **Create**." },
        { p: "If you are not sure which protocol to pick, start with Hysteria2. See [Protocols](/docs/protocols/) for the differences." },
        { h: "2. Add a client" },
        { p: "Open [Clients](/clients/) and press **Add client**. A name is enough. A traffic limit and an expiry date are optional." },
        { h: "3. Give access" },
        { p: "After the client is created the panel shows a QR code and a list of links, one per connection. Scan the code in the app on the phone, or copy the link and paste it into the app. Which app to use is described in [Apps](/docs/apps/)." },
        { note: "Each client gets access to all enabled connections. You do not need to create a connection per client." },
        { h: "What to do next" },
        { ul: [
          "Open the ports of your connections in the server firewall, see [Ports and firewall](/docs/firewall/).",
          "If you have a domain, turn on subscriptions so apps pick up changes by themselves, see [Clients and subscriptions](/docs/clients/).",
          "Save a backup of the data volume, see [Updates and backups](/docs/maintenance/).",
        ] },
      ],
      ru: [
        { p: "Portunus управляет прокси-сервером sing-box. Вы создаёте подключения (порты и протоколы, которые слушает сервер), добавляете клиентов (людей или устройства, которые подключаются) и выдаёте каждому клиенту ссылку или QR-код." },
        { h: "1. Создайте подключение" },
        { p: "Откройте [Подключения](/inbounds/) и нажмите **Добавить подключение**. Мастер проверит сервер и предложит настройки: свободный порт, сайт для маскировки, сертификат. Нажмите **Далее** несколько раз и затем **Создать**." },
        { p: "Если не знаете, какой протокол выбрать, начните с Hysteria2. Различия описаны в разделе [Протоколы](/docs/protocols/)." },
        { h: "2. Добавьте клиента" },
        { p: "Откройте [Клиенты](/clients/) и нажмите **Добавить клиента**. Достаточно имени. Лимит трафика и срок действия задавать необязательно." },
        { h: "3. Выдайте доступ" },
        { p: "После создания клиента панель покажет QR-код и список ссылок, по одной на каждое подключение. Отсканируйте код в приложении на телефоне или скопируйте ссылку и вставьте её в приложение. Какое приложение использовать, описано в разделе [Приложения](/docs/apps/)." },
        { note: "Каждый клиент получает доступ ко всем включённым подключениям. Создавать отдельное подключение для каждого клиента не нужно." },
        { h: "Что сделать дальше" },
        { ul: [
          "Откройте порты подключений в файрволе сервера, см. [Порты и файрвол](/docs/firewall/).",
          "Если есть домен, включите подписки, чтобы приложения сами получали изменения, см. [Клиенты и подписки](/docs/clients/).",
          "Сохраните резервную копию тома с данными, см. [Обновление и резервные копии](/docs/maintenance/).",
        ] },
      ],
    },
  },
  {
    slug: "protocols",
    title: { en: "Protocols", ru: "Протоколы" },
    summary: {
      en: "What each protocol is for and what it needs.",
      ru: "Для чего нужен каждый протокол и что ему требуется.",
    },
    body: {
      en: [
        { p: "You can run several protocols at once on different ports. Clients receive links to all of them, and sing-box apps switch to the one that works best." },
        { h: "Hysteria2" },
        { p: "Works over QUIC (UDP). Keeps its speed on links with packet loss or throttling, which makes it a good default for mobile and long-distance connections. Needs a certificate. To scanners the port looks like a regular HTTP/3 website: you choose which site they see." },
        { p: "Obfuscation hides that the traffic is QUIC at all. Turn it on only if Hysteria2 gets blocked, because not every app supports it." },
        { h: "VLESS Reality" },
        { p: "Works over TCP and imitates a visit to a real HTTPS site that you choose. It needs neither a certificate nor a domain. Fast on stable links. Choose a site that is reachable from the server with low latency: the wizard measures several and preselects the fastest." },
        { h: "TUIC" },
        { p: "Also works over QUIC (UDP), with less overhead than Hysteria2 and without its aggressive speed control. Needs a certificate." },
        { h: "Trojan" },
        { p: "TLS over TCP with a password. Supported by almost every app, which makes it a fallback for old devices. Needs a certificate." },
        { h: "Shadowsocks 2022" },
        { p: "Encryption without TLS and without imitating anything. Low overhead, suitable where traffic is not filtered. No certificate needed." },
        { h: "Ports" },
        { p: "TCP and UDP ports are separate, so VLESS Reality on TCP 443 and Hysteria2 on UDP 443 can run side by side. The wizard skips ports that are already taken on the server." },
      ],
      ru: [
        { p: "Можно запустить несколько протоколов одновременно на разных портах. Клиенты получают ссылки на все, а приложения на sing-box сами переключаются на тот, который работает лучше." },
        { h: "Hysteria2" },
        { p: "Работает поверх QUIC (UDP). Сохраняет скорость на линиях с потерями пакетов и ограничениями, поэтому подходит по умолчанию для мобильной связи и дальних соединений. Нужен сертификат. Для сканеров порт выглядит как обычный сайт на HTTP/3: вы выбираете, какой сайт они увидят." },
        { p: "Обфускация скрывает, что это вообще QUIC. Включайте её, только если Hysteria2 блокируют: её поддерживают не все приложения." },
        { h: "VLESS Reality" },
        { p: "Работает поверх TCP и выглядит как посещение настоящего HTTPS-сайта, который вы выбираете. Не требует ни сертификата, ни домена. Быстрый на стабильных линиях. Выбирайте сайт, до которого от сервера небольшая задержка: мастер замеряет несколько и заранее выбирает самый быстрый." },
        { h: "TUIC" },
        { p: "Тоже работает поверх QUIC (UDP), накладных расходов меньше, чем у Hysteria2, и нет агрессивного управления скоростью. Нужен сертификат." },
        { h: "Trojan" },
        { p: "TLS поверх TCP с паролем. Поддерживается почти всеми приложениями, поэтому подходит как запасной вариант для старых устройств. Нужен сертификат." },
        { h: "Shadowsocks 2022" },
        { p: "Шифрование без TLS и без маскировки. Небольшие накладные расходы, подходит там, где трафик не фильтруют. Сертификат не нужен." },
        { h: "Порты" },
        { p: "Порты TCP и UDP независимы, поэтому VLESS Reality на TCP 443 и Hysteria2 на UDP 443 работают рядом. Мастер пропускает порты, которые уже заняты на сервере." },
      ],
    },
  },
  {
    slug: "certificates",
    title: { en: "Certificates", ru: "Сертификаты" },
    summary: {
      en: "Three ways to get a certificate for Hysteria2, TUIC and Trojan.",
      ru: "Три способа получить сертификат для Hysteria2, TUIC и Trojan.",
    },
    body: {
      en: [
        { p: "Hysteria2, TUIC and Trojan encrypt with TLS and need a certificate. The wizard asks where it comes from. You can change the choice later by opening the connection in [Connections](/inbounds/)." },
        { h: "Let's Encrypt" },
        { p: "A free public certificate that every app accepts. It requires a domain whose A record points to this server. The certificate is issued after the connection is created and is renewed automatically." },
        { p: "Let's Encrypt has to verify that the domain is yours. There are three ways, chosen in the wizard or in [Settings](/settings/):" },
        { ul: [
          "**Port 80 is free.** Nothing else is needed, the server answers the check itself.",
          "**Port 80 is used by your web server** (Nginx, Nginx Proxy Manager, Caddy, Traefik). Add the block shown by the panel to the configuration of that domain, then press **Check forwarding**.",
          "**Cloudflare DNS.** No open ports are needed. Create an API token with the Zone.DNS edit permission and paste it into the panel.",
        ] },
        { p: "The block for Nginx looks like this (the port is shown in the panel):" },
        { code: "location /.well-known/acme-challenge/ {\n    proxy_pass http://127.0.0.1:8088;\n    proxy_set_header Host $host;\n}" },
        { note: "Until the certificate is issued the Connections table shows that it is pending. If that lasts more than a few minutes, look at the sing-box log in Settings." },
        { h: "Portunus certificate" },
        { p: "Issued by the panel itself for a domain or an IP address. It works immediately and needs nothing from you. sing-box profiles and subscriptions trust it automatically, and regular links pin this exact certificate." },
        { p: "Some apps accept only a verified certificate. For them, install the panel's certificate authority on the device once, see [Installing the certificate](/docs/ca/). After that you can turn on **Devices have the Portunus CA installed** for the connection, and its links will verify the certificate in the usual way." },
        { h: "Existing files" },
        { p: "Use this when certbot, Nginx Proxy Manager or Caddy already keeps a certificate for the domain. Mount the directory into the container read-only and give the paths inside the container:" },
        { code: "volumes:\n  - portunus-data:/var/lib/portunus\n  - /etc/letsencrypt:/certs:ro" },
        { p: "With certbot the paths are `/certs/live/<domain>/fullchain.pem` and `/certs/live/<domain>/privkey.pem`. The files must be readable by user 10001 inside the container. Renewed files are picked up automatically." },
      ],
      ru: [
        { p: "Hysteria2, TUIC и Trojan шифруют трафик через TLS, и им нужен сертификат. Мастер спрашивает, откуда его взять. Выбор можно изменить позже, открыв подключение в разделе [Подключения](/inbounds/)." },
        { h: "Let's Encrypt" },
        { p: "Бесплатный публичный сертификат, который принимают все приложения. Нужен домен, A-запись которого указывает на этот сервер. Сертификат выпускается после создания подключения и продлевается сам." },
        { p: "Let's Encrypt должен убедиться, что домен ваш. Есть три способа, они выбираются в мастере или в [Настройках](/settings/):" },
        { ul: [
          "**Порт 80 свободен.** Больше ничего не нужно, сервер сам ответит на проверку.",
          "**Порт 80 занят вашим веб-сервером** (Nginx, Nginx Proxy Manager, Caddy, Traefik). Добавьте блок, который покажет панель, в конфигурацию этого домена и нажмите **Проверить проброс**.",
          "**Cloudflare DNS.** Открытые порты не нужны. Создайте API-токен с правом Zone.DNS edit и вставьте его в панель.",
        ] },
        { p: "Блок для Nginx выглядит так (порт показан в панели):" },
        { code: "location /.well-known/acme-challenge/ {\n    proxy_pass http://127.0.0.1:8088;\n    proxy_set_header Host $host;\n}" },
        { note: "Пока сертификат не выпущен, в таблице подключений написано, что он ожидается. Если это длится дольше нескольких минут, посмотрите лог sing-box в Настройках." },
        { h: "Сертификат Portunus" },
        { p: "Выпускается самой панелью для домена или IP-адреса. Работает сразу и ничего от вас не требует. Профили sing-box и подписки доверяют ему автоматически, а обычные ссылки закрепляют именно этот сертификат." },
        { p: "Некоторые приложения принимают только проверенный сертификат. Для них один раз установите на устройство центр сертификации панели, см. [Установка сертификата](/docs/ca/). После этого для подключения можно включить **На устройства установлен сертификат Portunus**, и его ссылки будут проверять сертификат обычным способом." },
        { h: "Готовые файлы" },
        { p: "Подходит, если certbot, Nginx Proxy Manager или Caddy уже получают сертификат для домена. Подключите каталог в контейнер только для чтения и укажите пути внутри контейнера:" },
        { code: "volumes:\n  - portunus-data:/var/lib/portunus\n  - /etc/letsencrypt:/certs:ro" },
        { p: "Для certbot пути такие: `/certs/live/<домен>/fullchain.pem` и `/certs/live/<домен>/privkey.pem`. Файлы должны быть доступны для чтения пользователю 10001 внутри контейнера. Обновлённые файлы подхватываются сами." },
      ],
    },
  },
  {
    slug: "ca",
    title: { en: "Installing the certificate", ru: "Установка сертификата" },
    summary: {
      en: "How to install the Portunus certificate authority on a device.",
      ru: "Как установить центр сертификации Portunus на устройство.",
    },
    body: {
      en: [
        { p: "This applies only to connections that use the Portunus certificate. You install the certificate authority once per device. Server certificates issued by it are renewed by the panel, and the device keeps trusting them." },
        { p: "Download the file in [Settings](/settings/), in the **Certificate authority** card. If subscriptions are on, the card also shows a QR code that a phone can scan to download the file." },
        { note: "Before trusting the certificate, compare the SHA-256 fingerprint the device shows with the one in Settings. They must match." },
        { h: "iPhone and iPad" },
        { ol: [
          "Open the downloaded file and allow the profile to be downloaded.",
          "Go to Settings, tap **Profile Downloaded** and install it.",
          "Go to Settings, General, About, Certificate Trust Settings and turn on full trust for the Portunus certificate.",
        ] },
        { h: "Android" },
        { ol: [
          "Open Settings, Security, Encryption and credentials.",
          "Choose **Install a certificate**, then **CA certificate**, and pick the downloaded file.",
        ] },
        { p: "Apps built on sing-box (Hiddify, NekoBox, sing-box for Android) do not use certificates installed by the user. Give them the subscription or a regular link: those already contain everything needed." },
        { h: "Windows" },
        { ol: [
          "Download the `.cer` file and open it.",
          "Press **Install Certificate** and choose **Local Machine**.",
          "Choose **Place all certificates in the following store** and select **Trusted Root Certification Authorities**.",
        ] },
        { h: "macOS" },
        { ol: [
          "Open the `.crt` file. Keychain Access starts.",
          "Add the certificate to the **System** keychain.",
          "Open the certificate, expand **Trust** and choose **Always Trust**.",
        ] },
      ],
      ru: [
        { p: "Это нужно только для подключений с сертификатом Portunus. Центр сертификации устанавливается на устройство один раз. Сертификаты серверов, выпущенные им, панель продлевает сама, и устройство продолжает им доверять." },
        { p: "Файл скачивается в [Настройках](/settings/), в карточке **Центр сертификации**. Если включены подписки, там же есть QR-код: отсканируйте его телефоном, чтобы скачать файл." },
        { note: "Перед тем как доверять сертификату, сравните отпечаток SHA-256, который показывает устройство, с отпечатком в Настройках. Они должны совпасть." },
        { h: "iPhone и iPad" },
        { ol: [
          "Откройте скачанный файл и разрешите загрузку профиля.",
          "Откройте Настройки, нажмите **Профиль загружен** и установите его.",
          "Откройте Настройки, Основные, Об этом устройстве, Доверие сертификатам и включите полное доверие для сертификата Portunus.",
        ] },
        { h: "Android" },
        { ol: [
          "Откройте Настройки, Безопасность, Шифрование и учётные данные.",
          "Выберите **Установить сертификат**, затем **Сертификат ЦС** и укажите скачанный файл.",
        ] },
        { p: "Приложения на sing-box (Hiddify, NekoBox, sing-box для Android) не используют сертификаты, установленные пользователем. Давайте им подписку или обычную ссылку: в них уже есть всё нужное." },
        { h: "Windows" },
        { ol: [
          "Скачайте файл `.cer` и откройте его.",
          "Нажмите **Установить сертификат** и выберите **Локальный компьютер**.",
          "Выберите **Поместить все сертификаты в следующее хранилище** и укажите **Доверенные корневые центры сертификации**.",
        ] },
        { h: "macOS" },
        { ol: [
          "Откройте файл `.crt`. Запустится «Связка ключей».",
          "Добавьте сертификат в связку **Система**.",
          "Откройте сертификат, разверните **Доверие** и выберите **Всегда доверять**.",
        ] },
      ],
    },
  },
  {
    slug: "clients",
    title: { en: "Clients and subscriptions", ru: "Клиенты и подписки" },
    summary: {
      en: "Limits, expiry, replacing keys and automatic updates in apps.",
      ru: "Лимиты, сроки, замена ключей и автоматическое обновление в приложениях.",
    },
    body: {
      en: [
        { p: "A client is one person or one device. Each client has its own keys, so you can limit or disable one without touching the others." },
        { h: "Limits and expiry" },
        { ul: [
          "**Traffic limit** in gigabytes counts download and upload together. When it is reached, the client is disconnected and marked as over the limit.",
          "**Valid until** is the last day the client can connect.",
          "**Reset traffic** in the client's menu sets the counter back to zero, for example at the start of a month.",
          "**Allow connection** turns a client off without deleting it.",
        ] },
        { p: "Traffic is counted about every 30 seconds, so the numbers lag slightly behind." },
        { h: "Links and QR codes" },
        { p: "Open a client to see its links. There is one link per enabled connection, each with a QR code and a copy button. A link contains the client's keys: anyone who has it can connect as that client." },
        { h: "Replacing keys" },
        { p: "If a link got into the wrong hands, choose **Issue new keys** in the client's menu. The old links and the old subscription address stop working at once, and the client has to import the new ones." },
        { p: "**Replace keys** on a connection changes the keys of the server side. After that every client has to import its link again or refresh its subscription." },
        { h: "Subscriptions" },
        { p: "A subscription is one address that returns all of a client's connections. The app re-reads it from time to time, so new connections and replaced server keys reach the device without your help." },
        { p: "Turn subscriptions on in [Settings](/settings/) by setting a port, and open that port in the firewall. If the server has a domain with a Let's Encrypt certificate, subscriptions are served over HTTPS. Without one they are served over HTTP." },
        { p: "If you publish subscriptions through your own reverse proxy, enter its public address in the same card." },
        { note: "The subscription address is a secret, like a password. The long random part of the address is the only thing that protects it." },
      ],
      ru: [
        { p: "Клиентом считается один человек или одно устройство. У каждого клиента свои ключи, поэтому одного можно ограничить или отключить, не затрагивая остальных." },
        { h: "Лимиты и сроки" },
        { ul: [
          "**Лимит трафика** в гигабайтах считает скачанное и отправленное вместе. Когда он исчерпан, клиент отключается и помечается как превысивший лимит.",
          "**Действует до** задаёт последний день, когда клиент может подключаться.",
          "**Сбросить трафик** в меню клиента обнуляет счётчик, например в начале месяца.",
          "**Разрешить подключение** выключает клиента, не удаляя его.",
        ] },
        { p: "Трафик считается примерно раз в 30 секунд, поэтому цифры немного отстают." },
        { h: "Ссылки и QR-коды" },
        { p: "Откройте клиента, чтобы увидеть его ссылки. На каждое включённое подключение есть одна ссылка с QR-кодом и кнопкой копирования. В ссылке записаны ключи клиента: любой, у кого она есть, может подключиться от его имени." },
        { h: "Замена ключей" },
        { p: "Если ссылка попала не в те руки, выберите **Выдать новые ключи** в меню клиента. Старые ссылки и старый адрес подписки сразу перестанут работать, клиенту нужно импортировать новые." },
        { p: "**Заменить ключи** у подключения меняет ключи на стороне сервера. После этого каждому клиенту нужно заново импортировать ссылку или обновить подписку." },
        { h: "Подписки" },
        { p: "Подписка представляет собой один адрес, который отдаёт все подключения клиента. Приложение время от времени перечитывает его, поэтому новые подключения и заменённые ключи сервера попадают на устройство без вашего участия." },
        { p: "Включите подписки в [Настройках](/settings/), указав порт, и откройте этот порт в файрволе. Если у сервера есть домен с сертификатом Let's Encrypt, подписки отдаются по HTTPS. Без него они отдаются по HTTP." },
        { p: "Если вы публикуете подписки через свой reverse proxy, укажите его публичный адрес в той же карточке." },
        { note: "Адрес подписки нужно хранить в секрете, как пароль. Его защищает только длинная случайная часть адреса." },
      ],
    },
  },
  {
    slug: "apps",
    title: { en: "Apps", ru: "Приложения" },
    summary: {
      en: "What to install on a device and how to add a connection.",
      ru: "Что установить на устройство и как добавить подключение.",
    },
    body: {
      en: [
        { p: "Portunus works with apps that understand sing-box profiles or the common share links (`hysteria2://`, `vless://`, `tuic://`, `trojan://`, `ss://`)." },
        { h: "Apps on the sing-box core" },
        { p: "These support every protocol of the panel and get a full sing-box profile from a subscription, with automatic switching between connections:" },
        { ul: [
          "**sing-box** for Android, iOS and macOS, the official app.",
          "**Hiddify** for Android, iOS, Windows, macOS and Linux.",
          "**NekoBox** for Android.",
        ] },
        { h: "Other apps" },
        { p: "Apps such as v2rayN (Windows), v2rayNG (Android), Shadowrocket and Streisand (iOS) accept share links and subscriptions as a list of links. Check that the app supports the protocol you use: Hysteria2 and TUIC are not available everywhere." },
        { h: "Adding a connection" },
        { ol: [
          "Open the client in [Clients](/clients/).",
          "On a phone, choose adding from a QR code in the app and scan the code from the panel.",
          "On a computer, copy the link and choose import from the clipboard in the app.",
          "With subscriptions on, add the subscription address instead of single links. The app then keeps itself up to date.",
        ] },
        { note: "If an app refuses to connect to a connection with the Portunus certificate, either give it the subscription, or install the certificate authority on the device, see [Installing the certificate](/docs/ca/)." },
      ],
      ru: [
        { p: "Portunus работает с приложениями, которые понимают профили sing-box или обычные ссылки (`hysteria2://`, `vless://`, `tuic://`, `trojan://`, `ss://`)." },
        { h: "Приложения на ядре sing-box" },
        { p: "Они поддерживают все протоколы панели и получают по подписке полный профиль sing-box с автоматическим переключением между подключениями:" },
        { ul: [
          "**sing-box** для Android, iOS и macOS, официальное приложение.",
          "**Hiddify** для Android, iOS, Windows, macOS и Linux.",
          "**NekoBox** для Android.",
        ] },
        { h: "Другие приложения" },
        { p: "Приложения вроде v2rayN (Windows), v2rayNG (Android), Shadowrocket и Streisand (iOS) принимают ссылки и подписки в виде списка ссылок. Проверьте, что приложение поддерживает ваш протокол: Hysteria2 и TUIC есть не везде." },
        { h: "Как добавить подключение" },
        { ol: [
          "Откройте клиента в разделе [Клиенты](/clients/).",
          "На телефоне выберите в приложении добавление по QR-коду и отсканируйте код из панели.",
          "На компьютере скопируйте ссылку и выберите в приложении импорт из буфера обмена.",
          "Если включены подписки, добавьте адрес подписки вместо отдельных ссылок. Тогда приложение будет обновляться само.",
        ] },
        { note: "Если приложение отказывается подключаться к подключению с сертификатом Portunus, дайте ему подписку или установите на устройство центр сертификации, см. [Установка сертификата](/docs/ca/)." },
      ],
    },
  },
  {
    slug: "firewall",
    title: { en: "Ports and firewall", ru: "Порты и файрвол" },
    summary: {
      en: "Which ports to open and which to keep closed.",
      ru: "Какие порты открыть, а какие оставить закрытыми.",
    },
    body: {
      en: [
        { p: "The proxy listens on the server directly, so each connection needs its port opened in the server firewall and, if your provider has one, in the provider's firewall." },
        { h: "Open" },
        { ul: [
          "The port of every connection, for the transport shown in the [Connections](/inbounds/) table: TCP, UDP or both.",
          "The subscription port (TCP), if subscriptions are on.",
          "TCP 80, if Let's Encrypt verifies the domain directly on this server.",
        ] },
        { h: "Keep closed" },
        { ul: [
          "The panel port (2053 by default). It listens only on the server itself.",
          "Port 2054, the internal statistics service.",
        ] },
        { h: "Examples" },
        { p: "With ufw, for Hysteria2 on UDP 443 and VLESS Reality on TCP 443:" },
        { code: "sudo ufw allow 443/udp\nsudo ufw allow 443/tcp" },
        { h: "QUIC speed" },
        { p: "For Hysteria2 and TUIC it helps to raise the UDP buffer limits of the server:" },
        { code: "sudo sysctl -w net.core.rmem_max=16777216 net.core.wmem_max=16777216" },
        { p: "To keep the setting after a reboot, put both values into a file in `/etc/sysctl.d/`." },
      ],
      ru: [
        { p: "Прокси слушает порты прямо на сервере, поэтому порт каждого подключения нужно открыть в файрволе сервера и, если он есть, в файрволе провайдера." },
        { h: "Открыть" },
        { ul: [
          "Порт каждого подключения для транспорта из таблицы [Подключения](/inbounds/): TCP, UDP или оба.",
          "Порт подписок (TCP), если подписки включены.",
          "TCP 80, если Let's Encrypt проверяет домен напрямую на этом сервере.",
        ] },
        { h: "Оставить закрытыми" },
        { ul: [
          "Порт панели (по умолчанию 2053). Он слушает только на самом сервере.",
          "Порт 2054, внутренний сервис статистики.",
        ] },
        { h: "Примеры" },
        { p: "Для ufw, если Hysteria2 работает на UDP 443, а VLESS Reality на TCP 443:" },
        { code: "sudo ufw allow 443/udp\nsudo ufw allow 443/tcp" },
        { h: "Скорость QUIC" },
        { p: "Для Hysteria2 и TUIC полезно увеличить лимиты UDP-буферов сервера:" },
        { code: "sudo sysctl -w net.core.rmem_max=16777216 net.core.wmem_max=16777216" },
        { p: "Чтобы настройка сохранилась после перезагрузки, запишите оба значения в файл в каталоге `/etc/sysctl.d/`." },
      ],
    },
  },
  {
    slug: "access",
    title: { en: "Access to the panel", ru: "Доступ к панели" },
    summary: {
      en: "SSH tunnel, a lost password and signing out everywhere.",
      ru: "SSH-туннель, забытый пароль и выход на всех устройствах.",
    },
    body: {
      en: [
        { p: "By default the panel is reachable only from the server itself, on `127.0.0.1:2053`. This keeps the sign-in page away from the internet." },
        { h: "SSH tunnel" },
        { p: "Run this on your computer and keep it running:" },
        { code: "ssh -N -L 2053:127.0.0.1:2053 user@your-server" },
        { p: "Then open `http://localhost:2053` in the browser." },
        { h: "Password" },
        { p: "Change the password in [Account](/account/). All other sessions are signed out when you do." },
        { p: "If the password is lost, set a new one from the server. The command prints a new one-time password:" },
        { code: "docker compose exec portunus portunus reset-password" },
        { h: "Another address" },
        { p: "The listening address is set by `PORTUNUS_LISTEN` in the `.env` file next to `docker-compose.yml`. If you put the panel behind your own reverse proxy with HTTPS, keep it on a loopback address and let the proxy forward to it. Do not expose the panel port to the internet directly." },
        { note: "When the panel listens on an address that is not loopback, the Overview page shows a warning." },
      ],
      ru: [
        { p: "По умолчанию панель доступна только с самого сервера, по адресу `127.0.0.1:2053`. Так страница входа не видна из интернета." },
        { h: "SSH-туннель" },
        { p: "Выполните на своём компьютере и не закрывайте:" },
        { code: "ssh -N -L 2053:127.0.0.1:2053 user@your-server" },
        { p: "После этого откройте в браузере `http://localhost:2053`." },
        { h: "Пароль" },
        { p: "Пароль меняется в разделе [Аккаунт](/account/). При смене все остальные сеансы завершаются." },
        { p: "Если пароль потерян, задайте новый с сервера. Команда напечатает новый одноразовый пароль:" },
        { code: "docker compose exec portunus portunus reset-password" },
        { h: "Другой адрес" },
        { p: "Адрес, на котором слушает панель, задаётся переменной `PORTUNUS_LISTEN` в файле `.env` рядом с `docker-compose.yml`. Если вы ставите панель за свой reverse proxy с HTTPS, оставьте её на loopback-адресе, а прокси пусть пересылает запросы на него. Не открывайте порт панели в интернет напрямую." },
        { note: "Если панель слушает не на loopback-адресе, на странице «Обзор» появляется предупреждение." },
      ],
    },
  },
  {
    slug: "troubleshooting",
    title: { en: "If something does not work", ru: "Если что-то не работает" },
    summary: {
      en: "The usual causes, in the order worth checking.",
      ru: "Частые причины в том порядке, в котором их стоит проверять.",
    },
    body: {
      en: [
        { h: "The app does not connect" },
        { ol: [
          "Check that the port is open in the firewall for the right transport. Hysteria2 and TUIC need UDP, see [Ports and firewall](/docs/firewall/).",
          "Check the server address in [Settings](/settings/). It is written into every link. After changing it, clients have to import the links again.",
          "Check that the client is active in [Clients](/clients/): not disabled, not expired, not over its limit.",
          "Check that sing-box is running on the [Overview](/) page.",
          "Try another protocol. Some networks block UDP entirely, and then only VLESS Reality and Trojan work.",
        ] },
        { h: "The app complains about the certificate" },
        { p: "The connection uses the Portunus certificate and the app wants a verified one. Give the app the subscription, install the certificate authority on the device, or switch the connection to Let's Encrypt. See [Certificates](/docs/certificates/)." },
        { h: "The Let's Encrypt certificate is not issued" },
        { ul: [
          "The domain must point to this server. The wizard shows what the domain resolves to.",
          "With verification over port 80, the port must be reachable from the internet.",
          "With verification through your web server, press **Check forwarding** in the wizard.",
          "The reason is written in the sing-box log in [Settings](/settings/).",
        ] },
        { h: "sing-box does not start" },
        { p: "The Overview page says that the configuration was not applied, and Settings shows the error text. The most common cause is a port that another program on the server took after the connection was created. Change the port of the connection or free it." },
        { h: "Traffic is not counted" },
        { p: "Counters are updated about every 30 seconds. If they stay at zero while the client is connected, restart sing-box in Settings." },
      ],
      ru: [
        { h: "Приложение не подключается" },
        { ol: [
          "Проверьте, что порт открыт в файрволе для нужного транспорта. Hysteria2 и TUIC требуют UDP, см. [Порты и файрвол](/docs/firewall/).",
          "Проверьте адрес сервера в [Настройках](/settings/). Он записывается в каждую ссылку. После его изменения клиентам нужно импортировать ссылки заново.",
          "Проверьте в разделе [Клиенты](/clients/), что клиент активен: не отключён, срок не истёк, лимит не превышен.",
          "Проверьте на странице [Обзор](/), что sing-box работает.",
          "Попробуйте другой протокол. В некоторых сетях UDP закрыт полностью, тогда работают только VLESS Reality и Trojan.",
        ] },
        { h: "Приложение ругается на сертификат" },
        { p: "Подключение использует сертификат Portunus, а приложение требует проверенный. Дайте приложению подписку, установите на устройство центр сертификации или переведите подключение на Let's Encrypt. См. [Сертификаты](/docs/certificates/)." },
        { h: "Сертификат Let's Encrypt не выпускается" },
        { ul: [
          "Домен должен указывать на этот сервер. Мастер показывает, на какой адрес он указывает сейчас.",
          "При проверке через порт 80 этот порт должен быть доступен из интернета.",
          "При проверке через ваш веб-сервер нажмите **Проверить проброс** в мастере.",
          "Причина записана в логе sing-box в [Настройках](/settings/).",
        ] },
        { h: "sing-box не запускается" },
        { p: "На странице «Обзор» написано, что конфигурация не применена, а в Настройках показан текст ошибки. Самая частая причина: порт заняла другая программа на сервере уже после создания подключения. Измените порт подключения или освободите его." },
        { h: "Трафик не считается" },
        { p: "Счётчики обновляются примерно раз в 30 секунд. Если клиент подключён, а они остаются на нуле, перезапустите sing-box в Настройках." },
      ],
    },
  },
  {
    slug: "maintenance",
    title: { en: "Updates and backups", ru: "Обновление и резервные копии" },
    summary: {
      en: "How to update the panel and what to back up.",
      ru: "Как обновить панель и что сохранять.",
    },
    body: {
      en: [
        { h: "What is stored" },
        { p: "Everything lives in the Docker volume `portunus-data`:" },
        { ul: [
          "`portunus.db` with its `-wal` and `-shm` files: the database.",
          "`master.key`: the key that encrypts the private keys in the database.",
          "`sing-box/`: the generated configuration and the Let's Encrypt certificates.",
        ] },
        { note: "Back up `master.key` together with the database. Without it the connections cannot be decrypted and have to be created again." },
        { h: "Backup" },
        { code: "docker compose stop portunus\ndocker run --rm -v portunus_portunus-data:/data -v \"$PWD\":/backup alpine \\\n  tar czf /backup/portunus-backup.tgz -C /data .\ndocker compose start portunus" },
        { h: "Update" },
        { code: "git pull\ndocker compose up -d --build" },
        { p: "The database is upgraded automatically on start. A downgrade is not supported, so make a backup before updating." },
        { h: "State and logs" },
        { ul: [
          "`docker compose ps` shows whether the container is healthy.",
          "`docker compose logs portunus` shows the panel log.",
          "The sing-box log is in [Settings](/settings/).",
        ] },
      ],
      ru: [
        { h: "Что хранится" },
        { p: "Все данные лежат в томе Docker `portunus-data`:" },
        { ul: [
          "`portunus.db` и файлы `-wal` и `-shm` рядом с ним: база данных.",
          "`master.key`: ключ, которым зашифрованы закрытые ключи в базе.",
          "`sing-box/`: сгенерированная конфигурация и сертификаты Let's Encrypt.",
        ] },
        { note: "Сохраняйте `master.key` вместе с базой. Без него подключения нельзя расшифровать, их придётся создавать заново." },
        { h: "Резервная копия" },
        { code: "docker compose stop portunus\ndocker run --rm -v portunus_portunus-data:/data -v \"$PWD\":/backup alpine \\\n  tar czf /backup/portunus-backup.tgz -C /data .\ndocker compose start portunus" },
        { h: "Обновление" },
        { code: "git pull\ndocker compose up -d --build" },
        { p: "База данных обновляется сама при запуске. Откат на старую версию не поддерживается, поэтому перед обновлением сделайте резервную копию." },
        { h: "Состояние и логи" },
        { ul: [
          "`docker compose ps` показывает, здоров ли контейнер.",
          "`docker compose logs portunus` показывает лог панели.",
          "Лог sing-box находится в [Настройках](/settings/).",
        ] },
      ],
    },
  },
]

export function findDoc(slug: string): DocTopic {
  return docs.find((d) => d.slug === slug) ?? docs[0]
}
