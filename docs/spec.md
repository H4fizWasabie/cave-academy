# The Cave Academy website: v1 spec

Client: The Cave Academy (caveacademy.my). Target: 10 Dec 2026. All public copy is Malay.

## Goal

A marketing site for The Cave Academy plus its highlight feature, the **AI NLP Bots**: three niche chatbots that signed-up users can chat with.

## Stack

- Go (stdlib `net/http`, `html/template`, `embed`), one binary.
- SQLite through `modernc.org/sqlite` (pure Go, no cgo). Schema created on start (`CREATE TABLE IF NOT EXISTS`).
- Plain CSS, no framework, no JS build step. Small vanilla JS only where needed (mobile menu, chat).
- Tests: `go test ./...` (wrapped by `./test.sh`).

## Configuration (env)

| Var | Purpose | Default |
| --- | --- | --- |
| `ADDR` | listen address | `:8080` |
| `DB_PATH` | SQLite file | `cave-academy.db` |
| `OPENROUTER_API_KEY` | chatbot answers | unset: chat replies with a "not configured" message |
| `OPENROUTER_MODEL` | OpenRouter model id | `openai/gpt-6-luna` |
| `BREVO_API_KEY` | sends email (Brevo transactional API) | unset: email is logged to stdout instead |
| `MAIL_FROM` | sender address | `The Cave Academy <noreply@caveacademy.my>` |
| `ADMIN_EMAIL` | receives form submissions | `adam.caveconsult@gmail.com` |
| `BASE_URL` | public origin for canonical links and `/sitemap.xml` | `https://caveacademy.my` |
| `APP_ENV` | `development` or `production`. Production refuses to start without `OPENROUTER_API_KEY`, `BREVO_API_KEY`, `BASE_URL` and a valid `MAIL_FROM` | `development` |
| `CHAT_DAILY_CAP` | OpenRouter answers per KL day across all users (cost guard); over it the chat says it is busy | `2000` |

Contact details on the site: WhatsApp / phone 012-394 7200 (`https://wa.me/60123947200`), email adam.caveconsult@gmail.com.

## Pages (routes)

| Route | Page | Mockup |
| --- | --- | --- |
| `/` | Utama (home) | `docs/mockups/home-*.jpg` |
| `/corporate-training` | Corporate Training & RFQ, with RFQ form | `corporate-*.jpg` |
| `/ai-nlp-bots` | AI NLP Bots landing | `bots-*.jpg` |
| `/ai-nlp-bots/{educators,sales-marketing,hr}` | chat with one bot | none: same visual language |
| `/produk-digital` | Produk Digital catalogue (static, no checkout in v1) | `produk-*.jpg` |
| `/tentang-kami` | About | none |
| `/hubungi-kami` | Contact form + details | none |
| `/trainer-panel` | "Daftar Trainer Panel" signup form | none (home CTA links here) |
| `/daftar`, `/log-masuk`, `/log-keluar` | sign up, log in, log out | none |

Header nav: Utama / Program (dropdown: Corporate Training & RFQ, Trainer Panel) / AI NLP Bots (dropdown: three bots) / Produk Digital / Tentang Kami, a search icon (may be omitted in v1), "Hubungi Kami" and a green "WhatsApp Chat" button. Mobile: logo, WhatsApp icon, Hubungi Kami, hamburger menu.

## Design

Match the mockups: navy (~#0f1a3c) headings and dark buttons, yellow/gold (~#f5c518 to #e0a800 gradient) accent buttons, highlights and section-title dashes, white cards with soft shadows and rounded corners, WhatsApp green (#25d366). Heavy geometric sans for headings (Poppins, loaded from Google Fonts for now; self-host before launch if wanted), clean sans for body. Mobile-first, responsive to desktop.

Recurring blocks: hero (title with a yellow-highlighted second line, subtitle, primary yellow CTA, outlined "Lihat ... (2 Minit)" button, hero photo); a feature strip of 4 to 5 icon+label items; section title with a yellow dash; image cards with a diagonal yellow edge; a pale-yellow CTA banner with a navy pill button.

Assets: no real logo or photos supplied yet. Crop the round CAVE badge and the hero photos from the mockups as placeholders (`internal/site/static/img/`, embedded in the binary), to be replaced by the client's files.

## Accounts

- Sign up with name, email, password (bcrypt). Log in with email + password. Session cookie (random token stored in DB, HttpOnly, SameSite=Lax, Secure when behind HTTPS).
- No email verification or password reset in v1.
- `users.plan` is `free` or `paid`. Nothing sets `paid` in v1 except a direct DB update; the paid tiers and the BCL checkout come later.

## AI NLP Bots (chat)

- Three bots: **Educators**, **Sales & Marketing**, **HR & People Management**. Each has a Malay system prompt grounded in NLP (neuro-linguistic programming) coaching for that niche, owned by the server.
- The visitor picks the bot (no router). Chat requires login; a logged-out visitor sees the bot page with a prompt to sign up / log in.
- **Free plan: 3 questions per day** per user (day boundary Asia/Kuala_Lumpur), counted server-side. `paid` users: no limit in v1. When the limit is hit, say so and point to WhatsApp for plans.
- The server calls OpenRouter chat completions (`https://openrouter.ai/api/v1/chat/completions`), non-streaming, with `max_tokens` capped (~800), a timeout, and the last ~10 turns of the conversation. Messages are stored per user per bot so history survives reload.
- Bots may point users to site pages (Corporate Training, Produk Digital) and to WhatsApp. No RAG in v1; it comes when the client sends source documents.

## Forms and email

- RFQ (on `/corporate-training`): organisation, contact name, email, phone, training area (select), participants, preferred date, notes.
- Trainer panel (`/trainer-panel`): name, email, phone, expertise, years of experience, short bio.
- Contact (`/hubungi-kami`): name, email, message.
- Each submission is saved to SQLite and emailed to `ADMIN_EMAIL` via Brevo (`POST https://api.brevo.com/v3/smtp/email`). The sender domain must be authenticated in Brevo (caveacademy.my is not yet; the free plan's 300 emails a day are shared with CAVELMS). Server-side validation; a thank-you message on success. Basic spam guard: a hidden honeypot field.

## Out of scope for v1

Trainer management system (profiles, PDF, email replies), paid tiers and BCL checkout, RAG, admin UI, search, deploy.
