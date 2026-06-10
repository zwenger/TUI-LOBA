# Loba

A multiplayer Argentine rummy card game playable over TCP in your terminal.
One player hosts; everyone else joins. No servers to configure, no accounts —
just build and play.

```
▄▀ L O B A ▀▄  ◈  Argentine Rummy
```

---

## Rules

### Deck
108 cards: two standard French 52-card decks plus 4 jokers.

### Deal
Each player receives **9 cards**. One card is flipped face-up to start the
discard pile; the rest form the stock.

### Turn structure
1. **Draw** — take the top card from the stock, OR take the top card from the
   discard pile *(see Discard-pickup rule below)*.
2. **Meld / Lay off** (optional) — place valid melds from your hand onto the
   table, and/or lay off single cards onto any meld already on the table.
3. **Discard** — place exactly one card onto the discard pile to end your turn.

### Discard-pickup rule
You may only take the top discard card if you can **immediately use it this turn** —
either by forming a new meld that includes it, or by laying it off onto an
existing table meld. Taking it to speculate for future turns is not allowed.

Once you pick up the discard:
- **You must play that card** (meld or lay-off) before you can discard to end
  your turn.
- **You may not discard the picked-up card** itself.
- The card is highlighted in orange in your hand as a reminder.
- Other players see a log entry naming the card (it was public knowledge on the
  pile).

### Melds

**Pierna** (three of a kind)
- Exactly 3 cards of the same rank with **three different suits** to create it.
- After creation, additional cards of the same rank may be added regardless of suit.
- Jokers are **not** allowed in a pierna.

**Escalera** (run)
- 3 or more consecutive cards of the **same suit**.
- Ace may be low (A-2-3) or high (Q-K-A). No wrap-around (K-A-2 is illegal).
- Extendable on either end.
- At most **1 joker** per escalera. The joker is fixed once placed (it
  represents a specific card in the sequence).

### Lay-off
A player may lay off cards onto **any** meld on the table — their own or
anyone else's — but only after they have successfully melded at least once
themselves in the current round.

### Round end
A round ends when a player empties their hand (the final card may be a
discard). If the stock runs out, the discard pile (minus its top card) is
reshuffled into a new stock.

### Scoring
Penalty points for cards remaining in hand at round end:

| Card | Points |
|------|--------|
| Joker | 25 |
| Ace | 15 |
| K, Q, J | 10 |
| 2–10 | Face value |

Scores accumulate across rounds. When **any** player's cumulative total exceeds
**101**, the game ends and the player with the **lowest** total wins.

---

## Build

Requires Go 1.21+ (tested on Go 1.26).

```sh
git clone <repo>
cd loba
go build -o loba .
```

Or use the Makefile targets below.

---

## Play

### Host a game

```sh
./loba host --port 7777 --name Alvaro
```

- Starts a TCP server on port 7777 and joins as the first player.
- Share your IP (or hostname) and port with friends.
- Once everyone has joined, press **S** or **Enter** in the lobby to start.

### Join a game

```sh
./loba join 192.168.1.42:7777 --name Pablo
```

### Remote friends (LAN or port-forward)

For players on different networks, the host can either:
- Forward port 7777 on their router, or
- Use **Tailscale** (`tailscale up` on both machines, then join using the
  Tailscale IP shown by `tailscale ip -4`), or
- Use `--public` (see below) for a zero-config internet tunnel.

---

## Public rooms (bore.pub tunnel)

Play with anyone on the internet — no port forwarding, no router config,
no account, no token. Nothing to install or configure.

### Host a public room

```sh
./loba host --public --name Alvaro
```

That's it. While the tunnel is opening you'll see a status line. Once it's up:
- The public address is printed to your terminal before the TUI starts (stays in scrollback).
- The lobby screen shows a banner: **ROOM ADDRESS: bore.pub:XXXXX — share this with your friends**

The port number is assigned randomly each session — share the full address with your friends each time.

### Friends join

```sh
./loba join bore.pub:XXXXX --name Pablo
```

Exactly the same command as a LAN join — friends need nothing beyond the `loba` binary.

### Notes

- `bore.pub` is a free community service run by the bore project (<https://github.com/ekzhang/bore>).
  It requires no account and imposes no rate limits, but it is not an SLA-backed service.
- The assigned port changes every session. Share the address shown in the lobby each time you host.
- If bore.pub is unreachable, `--public` degrades gracefully: you'll see an error message and the
  game continues in **LAN-only mode** — local and Tailscale connections still work normally.

---

## Controls (game table)

| Key | Action |
|-----|--------|
| `←` / `h` | Move card cursor left |
| `→` / `l` | Move card cursor right |
| `Space` | Toggle card selection |
| `D` | Draw from stock |
| `T` | Take top of discard *(only if usable in a meld/lay-off)* |
| `M` | Meld selected cards as **pierna** |
| `E` | Meld selected cards as **escalera** |
| `0`–`9` | Lay off selected card(s) onto meld #N |
| `X` | Discard cursor card (ends turn) |
| `S` | Cycle hand sort mode: dealt → by rank → by suit |
| `Esc` | Clear selection |
| `Q` / `Ctrl+C` | Quit |

### Hand sorting
Press `S` to cycle through three display modes for your own hand:
- **sort:dealt** — cards in the order you received them (server order).
- **sort:rank** — sorted by rank (Ace through King), then by suit.
- **sort:suit** — sorted by suit (♠ ♥ ♦ ♣), then by rank.

Sorting is display-only: the active sort mode is shown in the help bar, and
the cursor follows the same logical card when you switch modes. All card
commands (meld, lay-off, discard) always reference the correct server-side
index regardless of display order.

---

## Disconnection handling

### Auto-play for disconnected players

If a player loses their connection mid-game, the server automatically plays
their turns while they are offline: it draws from the stock and immediately
discards the drawn card (after a ~1 s delay so other players can see the
flow). This happens every time the turn reaches them, not just once. If
consecutive players are all disconnected their turns chain automatically.
The event log shows a Spanish notice for each auto-played turn.

### Rejoin with the same name

A disconnected player can reconnect at any time using the same join command
with the same display name:

```sh
./loba join <host:port> --name SameName
```

The server matches the name case-insensitively against disconnected seats,
reattaches the connection, and sends a fresh state snapshot. The hand and
score are fully preserved. All other players receive a reconnect notice.

If a rejoin attempt arrives just before an auto-play timer fires, reconnecting
wins: the server checks the `Connected` flag before auto-playing.

**Errors:**

| Situation | Error |
|-----------|-------|
| Name not found among disconnected players | "la partida ya comenzó" |
| Name matches a currently-connected player | "ese nombre ya está en uso" |

---

## Makefile

```sh
make          # build for current platform
make all      # cross-compile all four targets
make darwin-arm64
make darwin-amd64
make linux-amd64
make windows-amd64
```
