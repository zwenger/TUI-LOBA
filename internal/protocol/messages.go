// Package protocol defines the newline-delimited JSON message types used for
// client↔server communication over TCP.
package protocol

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// ─── Command types (client → server) ─────────────────────────────────────────

const (
	CmdJoin        = "join"
	CmdStart       = "start"
	CmdDrawStock   = "draw_stock"
	CmdDrawDiscard = "draw_discard"
	CmdMeld        = "meld"
	CmdLayOff      = "lay_off"
	CmdDiscard     = "discard"
	CmdChat        = "chat"
	CmdNextRound   = "next_round"
	CmdClaimSeat   = "claim_seat" // reconnection: claim a disconnected seat by ID
)

// Command is a message sent from a client to the server.
type Command struct {
	Type string `json:"type"`

	// join
	Name string `json:"name,omitempty"`

	// claim_seat
	SeatID string `json:"seat_id,omitempty"`

	// meld / lay_off / discard
	CardIndexes []int  `json:"card_indexes,omitempty"`
	MeldType    string `json:"meld_type,omitempty"` // "pierna" | "escalera"
	MeldIndex   int    `json:"meld_index,omitempty"`
	CardIndex   int    `json:"card_index,omitempty"` // used by discard

	// chat
	Text string `json:"text,omitempty"`
}

// ─── Event types (server → client) ───────────────────────────────────────────

const (
	EvtState   = "state"
	EvtError   = "error"
	EvtMessage = "message"
	EvtSeats   = "seats" // server→client: list of available disconnected seats
)

// CardView is a client-visible representation of a card.
// Own cards are shown fully; opponents' cards have Rank/Suit hidden (Hidden=true).
type CardView struct {
	Rank       int    `json:"rank"`
	Suit       int    `json:"suit"`
	JokerIndex int    `json:"joker_index"`
	Hidden     bool   `json:"hidden,omitempty"`
	Label      string `json:"label,omitempty"` // pre-rendered short label, e.g. " 7♠"
}

// MeldView is a client-visible meld.
type MeldView struct {
	Index    int        `json:"index"`
	Type     string     `json:"type"` // "pierna" | "escalera"
	Cards    []CardView `json:"cards"`
	OwnerID  string     `json:"owner_id"`
	OwnerName string    `json:"owner_name"`
}

// PlayerView is a client-visible player summary.
type PlayerView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CardCount  int    `json:"card_count"`
	TotalScore int    `json:"total_score"`
	RoundScore int    `json:"round_score"`
	HasMelded  bool   `json:"has_melded"`
	Connected  bool   `json:"connected"`
	IsActive   bool   `json:"is_active"`
	IsSelf     bool   `json:"is_self"`
}

// RevealedPlayerHand holds one player's publicly-revealed hand for the
// round-summary and game-over screens. Cards are only revealed when the round
// is over; during normal play other players' hands remain hidden (count only).
type RevealedPlayerHand struct {
	PlayerID   string     `json:"player_id"`
	PlayerName string     `json:"player_name"`
	Cards      []CardView `json:"cards"`       // full card identities (public at round end)
	RoundScore int        `json:"round_score"` // penalty sum for this round (0 = round winner)
	TotalScore int        `json:"total_score"` // cumulative total after this round
	IsWinner   bool       `json:"is_winner"`   // true for the player who went out
}

// StateSnapshot is the full personalized game state sent to each client.
type StateSnapshot struct {
	Phase       string       `json:"phase"`
	Round       int          `json:"round"`
	ActiveID    string       `json:"active_id"`
	Players     []PlayerView `json:"players"`
	Hand        []CardView   `json:"hand"`         // only the recipient's hand
	Melds       []MeldView   `json:"melds"`
	DiscardTop  *CardView    `json:"discard_top"`
	StockCount  int          `json:"stock_count"`
	Events      []string     `json:"events"`
	WinnerID    string       `json:"winner_id,omitempty"`
	WinnerName  string       `json:"winner_name,omitempty"`
	// PickedUpDiscard is the card the active player took from the discard pile
	// this turn. Non-nil only for the player who picked it up (must play it
	// before discarding). Other clients receive it as nil.
	PickedUpDiscard *CardView `json:"picked_up_discard,omitempty"`
	// RoundReveal is populated only during the round_end and game_over phases.
	// It contains every player's remaining hand so the penalty sums can be
	// verified by all players. Nil during normal play to keep other players'
	// hands hidden.
	RoundReveal []RevealedPlayerHand `json:"round_reveal,omitempty"`
}

// Envelope wraps any server→client message.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ─── Lobby message ────────────────────────────────────────────────────────────

// LobbyState is sent while waiting for the host to start.
type LobbyState struct {
	Players    []string `json:"players"`
	HostID     string   `json:"host_id"`
	// PublicAddr is the bore.pub TCP address friends can use to join from outside
	// the LAN (e.g. "0.tcp.bore.pub.io:12345"). Empty when --public was not used.
	PublicAddr string `json:"public_addr,omitempty"`
}

// ─── Seat-picker messages ─────────────────────────────────────────────────────

// SeatEntry describes one available (disconnected) seat offered to a rejoining player.
type SeatEntry struct {
	ID        string `json:"id"`         // stable player ID (e.g. "p2")
	Name      string `json:"name"`       // display name of the original player
	CardCount int    `json:"card_count"` // cards currently in hand
	Score     int    `json:"score"`      // accumulated total score
}

// SeatsOffer is sent by the server when a client joins a game that has already
// started. The client presents the list and the player picks a seat to claim.
type SeatsOffer struct {
	Seats []SeatEntry `json:"seats"`
}

// ─── Wire helpers ─────────────────────────────────────────────────────────────

// WriteJSON encodes v as JSON and writes it followed by a newline.
func WriteJSON(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", data)
	return err
}

// ReadJSON reads one newline-delimited JSON object from r and decodes it into v.
func ReadJSON(r *bufio.Reader, v any) error {
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(line), v)
}

// SendEnvelope marshals payload and writes an Envelope.
func SendEnvelope(w io.Writer, evtType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return WriteJSON(w, Envelope{Type: evtType, Payload: raw})
}

// SendError sends an error envelope.
func SendError(w io.Writer, msg string) error {
	return SendEnvelope(w, EvtError, map[string]string{"message": msg})
}
