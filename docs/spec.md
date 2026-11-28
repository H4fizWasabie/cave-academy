# The Cave Academy website: v1 spec

Client: The Cave Academy (thecaveacademy.com). Target: 10 Dec 2026. All public copy is Malay.

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
| `MAIL_FROM` | sender address | `The Cave Academy <noreply@thecaveacademy.com>` |
| `ADMIN_EMAIL` | receives form submissions | `adam.caveconsult@gmail.com` |
| `BASE_URL` | public origin for canonical links and `/sitemap.xml` | `https://thecaveacademy.com` |
| `APP_ENV` | `development` or `production`. Production refuses to start without `OPENROUTER_API_KEY`, `BREVO_API_KEY`, `BASE_URL` and a valid `MAIL_FROM` | `development` |
| `INTRO_VIDEO_URL` | intro video for the "(2 Minit)" hero buttons (home, corporate, produk); they open it in a new tab | unset: the buttons are not shown |
| `BCL_WEBHOOK_KEY` | secret in the BCL webhook address, `/bcl/webhook/<key>`; at least 32 characters (`openssl rand -hex 24`) | unset: the webhook answers 404 |
| `BCL_API_URL` | BCL API base, for tests only (they point it at a fake BCL) | `https://bcl.my/api` |
| `BCL_API_TOKEN` | BCL API token with Read access; every paid order is read back from BCL with it. Production refuses to start with one of the two BCL vars set and not the other | unset: the webhook answers 404 |
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
| `/trainer` | directory of published Cave Trainers | none |
| `/trainer/profil` | a logged-in user's own trainer profile editor (GET, POST); logged out goes to the login page | none |
| `/trainer/{slug}`, `/trainer/{slug}/pdf` | public profile of a published Cave Trainer, and its PDF; anyone else's profile is a 404 | none |
| `/pelan` | Pelan Langganan: one card per active plan with a "Langgan" button to its BCL form; "akan datang" and WhatsApp when there is no active plan | none: same visual language |
| `POST /bcl/webhook/{key}` | BCL payment webhook (see Paid tiers) | none |
| `/daftar`, `/log-masuk`, `/log-keluar` | sign up, log in, log out | none |
| `/lupa-kata-laluan`, `/reset-kata-laluan` | password reset by email | none |
| `/dasar-privasi`, `/terma` | privacy policy, terms | none |
| `/admin`, `/admin/{rfq,trainer,mesej,pengguna,pelan,pengetahuan,trainer-profil}` | back office for admins (`users.is_admin`), CSV downloads; logged-out visitors are redirected to the login page, other accounts get a 404 | none |

Header nav: Utama / Program (dropdown: Corporate Training & RFQ, Trainer Panel) / AI NLP Bots (dropdown: the bots) / Produk Digital / Tentang Kami, a search icon (may be omitted in v1), "Hubungi Kami" and a green "WhatsApp Chat" button. Mobile: logo, WhatsApp icon, Hubungi Kami, hamburger menu.

## Design

Match the mockups: navy (~#0f1a3c) headings and dark buttons, yellow/gold (~#f5c518 to #e0a800 gradient) accent buttons, highlights and section-title dashes, white cards with soft shadows and rounded corners, WhatsApp green (#25d366). Heavy geometric sans for headings (Poppins, loaded from Google Fonts for now; self-host before launch if wanted), clean sans for body. Mobile-first, responsive to desktop.

Recurring blocks: hero (title with a yellow-highlighted second line, subtitle, primary yellow CTA, outlined "Lihat ... (2 Minit)" video button that appears only when `INTRO_VIDEO_URL` is set, hero photo); a feature strip of 4 to 5 icon+label items; section title with a yellow dash; image cards with a diagonal yellow edge; a pale-yellow CTA banner with a navy pill button.

Assets: no real logo or photos supplied yet. Crop the round CAVE badge and the hero photos from the mockups as placeholders (`internal/site/static/img/`, embedded in the binary), to be replaced by the client's files.

## Accounts

- Sign up with name, email, password (bcrypt). Log in with email + password. Session cookie (random token stored in DB, HttpOnly, SameSite=Lax, Secure when behind HTTPS).
- No email verification in v1. Password reset by email lives in `internal/site/reset.go` (`/lupa-kata-laluan`, `/reset-kata-laluan`).
- `users.plan` is `free` or `paid`. Only an admin (the users list in `/admin`) or a direct DB update sets `paid`; `paid` means unlimited questions, a manual override beside the paid tiers below.

## Paid tiers and BCL checkout

- Tiers are data: table `plans` (`name`, `price_sen`, `daily_questions` with 0 = unlimited, `days`, `bcl_link`, `active`, `sort`), managed by admins at `/admin/pelan` (create, edit, deactivate; never deleted). Nothing is seeded: with no active plan `/pelan` says plans are coming soon and offers WhatsApp.
- A user's daily limit is the best subscription running now (`subscriptions`: `user_id`, `plan_id`, `starts_at`, `ends_at`, `bcl_order` UNIQUE); unlimited beats any number. No running subscription gives the free 3. The bot page shows the plan and its end date.
- Checkout is BCL (bcl.my). A plan's `bcl_link` is its BCL payment form; `/pelan` sends logged-in users to it and logged-out users to `/daftar?next=/pelan` first. The payer must use the same email as their account.
- BCL calls `POST /bcl/webhook/<BCL_WEBHOOK_KEY>` (Advanced > Webhook Settings, Payment Success). BCL signs nothing, so the key only keeps strangers out and the body is trusted for the order number alone: the app reads the order back from `GET https://bcl.my/api/transactions/<order>` (Bearer `BCL_API_TOKEN`, 10 s timeout) and uses BCL's payer email, amounts and form. Only status 3, paid and live counts; the form is found through `/forms` by slug and matched to the plan whose `bcl_link` ends in that slug (inactive plans included); subtotal less discount must be at least the price.
- A paid order is granted once (`bcl_order` UNIQUE, repeats are no-ops) to the account with the payer's email, lowercased. Buying a plan the user still has runs on from its end; any other plan starts now. A paid order that cannot be granted (no account, a form that is no plan, too little paid) goes to `unmatched_payments`, shown on `/admin/pelan` where an admin can grant it by hand, and is answered 200. A wrong or unset key is 404; an order BCL does not know, has not settled or cannot be reached about is 500, so BCL retries. The access log shows `/bcl/webhook/…`, never the key.

## AI NLP Bots (chat)

- Four bots: **Educators**, **Sales & Marketing**, **HR & People Management**, **Pengurusan Stres** (stress management; carries a Notice that it is not therapy). Every bot first runs a server-side crisis check (crisis.go): a flagged message gets a fixed help-line reply and never reaches the model. Each has a Malay system prompt grounded in NLP (neuro-linguistic programming) coaching for that niche, owned by the server.
- The visitor picks the bot (no router). Chat requires login; a logged-out visitor sees the bot page with a prompt to sign up / log in.
- **Free plan: 3 questions per day** per user (day boundary Asia/Kuala_Lumpur), counted server-side. `paid` users and unlimited tiers: no limit; other tiers have their own daily number (see Paid tiers). When the limit is hit, say so and point to `/pelan` and WhatsApp.
- The server calls OpenRouter chat completions (`https://openrouter.ai/api/v1/chat/completions`), non-streaming, with `max_tokens` capped (~800), a timeout, and the last ~10 turns of the conversation. Messages are stored per user per bot so history survives reload.
- Bots may point users to site pages (Corporate Training, Produk Digital) and to WhatsApp.

### RAG over the client's material

- Admins add material at `/admin/pengetahuan`: a title, one bot or "Semua bot", and pasted text (up to 200 KB) or an uploaded `.txt` / `.md` file (up to 1 MB, UTF-8). Deleting a document removes its chunks.
- Each document is cut into chunks of about 1000 characters (paragraph, then sentence boundaries, with a small overlap) and stored in a SQLite FTS5 table (`knowledge_chunks`), next to `knowledge_docs`. No embeddings provider.
- On every question (`knowledge.go`) the question's words (lowercased, without stopwords and FTS syntax, each quoted) are searched with bm25 among the chunks of the current bot and of "all". The top 4 chunks, at most 3500 characters, are appended to the system prompt in a Malay "Rujukan" block that says the text is data, not instructions. No match: the prompt is unchanged.
- Only `/admin/pengetahuan` may POST a body above the 64 KB global cap (`middleware.go`); its handler sets its own cap after the admin check.

## Forms and email

- RFQ (on `/corporate-training`): organisation, contact name, email, phone, training area (select), participants, preferred date, notes.
- Trainer panel (`/trainer-panel`): name, email, phone, expertise, years of experience, short bio.
- Contact (`/hubungi-kami`): name, email, message.
- Each submission is saved to SQLite and emailed to `ADMIN_EMAIL` via Brevo (`POST https://api.brevo.com/v3/smtp/email`).  The notification's Reply-To is the submitter's email; mail to users carries Reply-To `ADMIN_EMAIL`. The sender domain must be authenticated in Brevo (thecaveacademy.com is not yet; the free plan's 300 emails a day are shared with CAVELMS). Server-side validation; a thank-you message on success. Basic spam guard: a hidden honeypot field.

## Trainer profiles

- Every account may have one profile at `/trainer/profil`: display name, headline, bio, expertise tags, years, phone, location, LinkedIn URL and one of three layouts (`klasik`, `moden`, `ringkas`), with a live preview. A new profile is a `member` and hidden.
- An admin promotes a profile to `cave_trainer` and publishes it at `/admin/trainer-profil`, and can email the trainer from there. Only a published Cave Trainer is public: listed on `/trainer`, shown at `/trainer/{slug}` with a PDF, and in `/sitemap.xml`. Any other slug is a 404.

## Out of scope for v1

Done since the first draft: paid tiers with BCL checkout, the admin panel, deploy scripts (staging and production, backups), password reset, legal pages, SEO, hardening (headers, rate limits, graceful shutdown) and Brevo email.

Still to come: real images from the client. The header search icon stays omitted.
