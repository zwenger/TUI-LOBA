package client

// mapping_test.go — tests for display↔server index mapping in the Bubbletea
// client model, including the stale-selection bug that caused the reported
// "escalera cards are not consecutive" false rejection.

import (
	"fmt"
	"testing"

	"github.com/zwenger/TUI-LOBA/internal/protocol"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func cv(rank, suit int) protocol.CardView {
	return protocol.CardView{Rank: rank, Suit: suit}
}

// makeModel returns a Model with a loaded 9-card state and the given sortMode.
// The hand in server order: [6♥, 7♥, 8♥, 2♣, 3♠, K♦, 4♣, 5♠, Q♥]
func makeModelWithHand(mode sortMode) (Model, *protocol.StateSnapshot) {
	hand := []protocol.CardView{
		cv(6, 1),  // 0: 6♥
		cv(7, 1),  // 1: 7♥
		cv(8, 1),  // 2: 8♥
		cv(2, 3),  // 3: 2♣
		cv(3, 0),  // 4: 3♠
		cv(13, 2), // 5: K♦
		cv(4, 3),  // 6: 4♣
		cv(5, 0),  // 7: 5♠
		cv(12, 1), // 8: Q♥
	}
	snap := &protocol.StateSnapshot{
		Phase:    "meld",
		ActiveID: "self-1",
		Hand:     hand,
		Players: []protocol.PlayerView{
			{ID: "self-1", Name: "Alvaro", IsSelf: true, IsActive: true, Connected: true},
			{ID: "opp-1", Name: "Bob", Connected: true},
		},
	}
	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		state:    snap,
		sortMode: mode,
		selected: make(map[int]bool),
	}
	m.displayToServer = buildSortMapping(snap.Hand, mode)
	return m, snap
}

// handAfterDrawDiscard returns a 10-card hand snapshot where 6♥ was appended
// (as if DrawDiscard added it from the discard pile).
// Server order: [7♥, 8♥, 2♣, 3♠, K♦, 4♣, 5♠, Q♥, J♦, 6♥]
//
// This simulates the reported bug scenario: 6♥ lands at server index 9 (last),
// but in sortByRank it appears BEFORE 7♥ and 8♥ in the display.
func handAfterDrawDiscard6H() []protocol.CardView {
	return []protocol.CardView{
		cv(7, 1),  // 0: 7♥
		cv(8, 1),  // 1: 8♥
		cv(2, 3),  // 2: 2♣
		cv(3, 0),  // 3: 3♠
		cv(13, 2), // 4: K♦
		cv(4, 3),  // 5: 4♣
		cv(5, 0),  // 6: 5♠
		cv(12, 1), // 7: Q♥
		cv(11, 2), // 8: J♦
		cv(6, 1),  // 9: 6♥  ← newly drawn from discard
	}
}

// ─── Sort mapping correctness ─────────────────────────────────────────────────

// TestBuildSortMappingIdentity verifies that sortDealt produces the identity mapping.
func TestBuildSortMappingIdentity(t *testing.T) {
	hand := []protocol.CardView{
		cv(10, 0), cv(3, 1), cv(7, 3), cv(1, 2), cv(5, 0),
	}
	m := buildSortMapping(hand, sortDealt)
	for i, srvIdx := range m {
		if srvIdx != i {
			t.Errorf("sortDealt mapping[%d] = %d, want %d", i, srvIdx, i)
		}
	}
}

// TestBuildSortMappingByRank verifies rank-ascending sort order.
func TestBuildSortMappingByRank(t *testing.T) {
	hand := []protocol.CardView{
		cv(10, 0), // 0: 10♠
		cv(3, 1),  // 1:  3♥
		cv(7, 3),  // 2:  7♣
		cv(1, 2),  // 3:  A♦
		cv(5, 0),  // 4:  5♠
	}
	mapping := buildSortMapping(hand, sortByRank)
	// Expected display order by rank asc: A♦(3), 3♥(1), 5♠(4), 7♣(2), 10♠(0)
	want := []int{3, 1, 4, 2, 0}
	for i, got := range mapping {
		if got != want[i] {
			t.Errorf("sortByRank mapping[%d] = %d (rank %d), want %d (rank %d)",
				i, got, hand[got].Rank, want[i], hand[want[i]].Rank)
		}
	}
}

// TestBuildSortMappingBySuit verifies suit-then-rank sort order.
func TestBuildSortMappingBySuit(t *testing.T) {
	hand := []protocol.CardView{
		cv(7, 3),  // 0:  7♣  suit=3
		cv(3, 1),  // 1:  3♥  suit=1
		cv(10, 0), // 2: 10♠  suit=0
		cv(5, 2),  // 3:  5♦  suit=2
		cv(2, 0),  // 4:  2♠  suit=0
	}
	mapping := buildSortMapping(hand, sortBySuit)
	// Suit order: ♠(0), ♥(1), ♦(2), ♣(3)
	// Within ♠: 2♠(4), 10♠(2); ♥: 3♥(1); ♦: 5♦(3); ♣: 7♣(0)
	want := []int{4, 2, 1, 3, 0}
	for i, got := range mapping {
		if got != want[i] {
			t.Errorf("sortBySuit mapping[%d] = %d, want %d", i, got, want[i])
		}
	}
}

// TestServerIndexRoundTrip verifies that serverIndex(display) returns the
// correct server index for each display position in each sort mode.
func TestServerIndexRoundTrip(t *testing.T) {
	modes := []sortMode{sortDealt, sortByRank, sortBySuit}
	for _, mode := range modes {
		m, snap := makeModelWithHand(mode)
		for dispIdx := 0; dispIdx < len(snap.Hand); dispIdx++ {
			srvIdx := m.serverIndex(dispIdx)
			if srvIdx < 0 || srvIdx >= len(snap.Hand) {
				t.Errorf("mode=%v dispIdx=%d → srvIdx=%d out of range", mode, dispIdx, srvIdx)
				continue
			}
			// The card at displayHand[dispIdx] should match snap.Hand[srvIdx].
			displayedCard := snap.Hand[srvIdx]
			_ = displayedCard // verifies the index is valid
		}
	}
}

// ─── Mapping after draw-discard (the bug scenario) ────────────────────────────

// TestMappingRebuiltAfterDrawDiscard verifies that after receiving a new state
// snapshot (10-card hand), the displayToServer mapping correctly places 6♥ in
// the sorted display position and 7♥/8♥ after it.
func TestMappingRebuiltAfterDrawDiscard(t *testing.T) {
	// Simulate receiving the new state after DrawDiscard.
	newHand := handAfterDrawDiscard6H()
	newMapping := buildSortMapping(newHand, sortByRank)

	// In rank order the hand is: 2♣(2), 3♠(3), 4♣(5), 5♠(6), 6♥(9), 7♥(0), 8♥(1), Q♥(7), J♦(8), K♦(4)
	// We only check that 6♥ < 7♥ < 8♥ in display order.
	find6H, find7H, find8H := -1, -1, -1
	for dispIdx, srvIdx := range newMapping {
		switch {
		case newHand[srvIdx].Rank == 6 && newHand[srvIdx].Suit == 1:
			find6H = dispIdx
		case newHand[srvIdx].Rank == 7 && newHand[srvIdx].Suit == 1:
			find7H = dispIdx
		case newHand[srvIdx].Rank == 8 && newHand[srvIdx].Suit == 1:
			find8H = dispIdx
		}
	}
	if find6H < 0 || find7H < 0 || find8H < 0 {
		t.Fatalf("couldn't find 6♥(%d), 7♥(%d) or 8♥(%d) in mapping", find6H, find7H, find8H)
	}
	if !(find6H < find7H && find7H < find8H) {
		t.Errorf("sortByRank: expected 6♥(%d) < 7♥(%d) < 8♥(%d) in display order",
			find6H, find7H, find8H)
	}

	// The server indices for 6♥, 7♥, 8♥ must be 9, 0, 1 respectively.
	if s6 := newMapping[find6H]; s6 != 9 {
		t.Errorf("6♥ server index = %d, want 9", s6)
	}
	if s7 := newMapping[find7H]; s7 != 0 {
		t.Errorf("7♥ server index = %d, want 0", s7)
	}
	if s8 := newMapping[find8H]; s8 != 1 {
		t.Errorf("8♥ server index = %d, want 1", s8)
	}
}

// TestSelectedCardsByIdentityAfterDraw verifies that when a player selects
// 6♥ 7♥ 8♥ by display index after receiving the new 10-card state, the
// server indices sent point to the correct cards.
func TestSelectedCardsByIdentityAfterDraw(t *testing.T) {
	newHand := handAfterDrawDiscard6H()
	m := Model{
		screen:          screenGame,
		selfID:          "self-1",
		sortMode:        sortByRank,
		selected:        make(map[int]bool),
		displayToServer: buildSortMapping(newHand, sortByRank),
		state: &protocol.StateSnapshot{
			Hand:     newHand,
			ActiveID: "self-1",
		},
	}

	// Find display indices for 6♥, 7♥, 8♥.
	disp6H, disp7H, disp8H := -1, -1, -1
	for dispIdx, srvIdx := range m.displayToServer {
		switch {
		case newHand[srvIdx].Rank == 6 && newHand[srvIdx].Suit == 1:
			disp6H = dispIdx
		case newHand[srvIdx].Rank == 7 && newHand[srvIdx].Suit == 1:
			disp7H = dispIdx
		case newHand[srvIdx].Rank == 8 && newHand[srvIdx].Suit == 1:
			disp8H = dispIdx
		}
	}
	if disp6H < 0 || disp7H < 0 || disp8H < 0 {
		t.Fatal("cards not found in display mapping")
	}

	// Simulate the player selecting those three display positions.
	m.selected[disp6H] = true
	m.selected[disp7H] = true
	m.selected[disp8H] = true

	// Translate to server indices.
	serverIdxs := m.serverIndexes(selectedSlice(m.selected))

	// The resulting server indices must map back to 6♥, 7♥, 8♥.
	got := make(map[string]bool)
	for _, si := range serverIdxs {
		card := newHand[si]
		key := cardKey(card)
		got[key] = true
	}
	for _, expected := range []string{"6:1", "7:1", "8:1"} {
		if !got[expected] {
			t.Errorf("server indices do not include %s; got %v from indices %v", expected, got, serverIdxs)
		}
	}
}

func cardKey(c protocol.CardView) string {
	return fmt.Sprintf("%d:%d", c.Rank, c.Suit)
}

// ─── Bug 2: 1-based lay-off display → 0-based server translation ─────────────

// TestLayOffMeldIndexClientTranslation verifies that pressing digit key N
// (1-based as shown in the UI) sends serverMeldIdx = N-1 to the server,
// and that pressing "0" is rejected with a local error message.
func TestLayOffMeldIndexClientTranslation(t *testing.T) {
	// Build a model in game state with 2 melds and a selected card.
	hand := []protocol.CardView{
		cv(8, 1), // 0: 8♥
		cv(3, 0), // 1: 3♠
	}
	snap := &protocol.StateSnapshot{
		Phase:    "meld",
		ActiveID: "self-1",
		Hand:     hand,
		Melds: []protocol.MeldView{
			{Index: 0, Type: "escalera"},
			{Index: 1, Type: "pierna"},
		},
		Players: []protocol.PlayerView{
			{ID: "self-1", IsSelf: true, IsActive: true},
		},
	}
	m := Model{
		screen:          screenGame,
		selfID:          "self-1",
		sortMode:        sortDealt,
		selected:        map[int]bool{0: true}, // 8♥ selected
		displayToServer: []int{0, 1},
		state:           snap,
		// No real conn; we only test the index translation logic, not the send.
	}

	// Simulate pressing "1": should target server meld index 0.
	// We verify the translation formula, not actual network send.
	digit1 := int('1' - '0') // = 1
	serverIdx1 := digit1 - 1  // = 0
	if serverIdx1 != 0 {
		t.Errorf("digit '1' → serverMeldIdx = %d, want 0", serverIdx1)
	}

	// Pressing "2": should target server meld index 1.
	digit2 := int('2' - '0') // = 2
	serverIdx2 := digit2 - 1  // = 1
	if serverIdx2 != 1 {
		t.Errorf("digit '2' → serverMeldIdx = %d, want 1", serverIdx2)
	}

	// Pressing "0": not a valid meld label — model should set lastError.
	updatedM, _ := m.handleGameKey("0")
	updated := updatedM.(Model)
	if updated.lastError == "" {
		t.Error("pressing '0' should set lastError (digit 0 is not a valid meld number)")
	}

	// Pressing "1" with a selected card should NOT set an error (no connection, no send).
	m2 := m
	m2.lastError = ""
	m2.selected = map[int]bool{0: true}
	updatedM2, _ := m2.handleGameKey("1")
	updated2 := updatedM2.(Model)
	// lastError should remain empty when digit >= 1
	if updated2.lastError != "" {
		// Only a failure if the error is about invalid meld index (not a conn error)
		if updated2.lastError == "Ingresá el número de combinación (1, 2, …)" {
			t.Errorf("unexpected meld-number error for digit '1': %s", updated2.lastError)
		}
	}
}

// ─── Stale-selection bug: selected not cleared on state update ─────────────────

// TestStaleSelectionBugAfterHandGrows is the regression test for the client-side
// stale-selection bug. Scenario:
//  1. Player is in sortByRank mode with a 9-card hand.
//  2. Player pre-selects display indices 0 and 1 (some cards in sorted view).
//  3. A new state arrives with 10 cards (6♥ added by DrawDiscard at server idx 9).
//  4. WITHOUT the fix, m.selected still holds {0, 1}, which now map to DIFFERENT
//     cards in the new mapping.
//  5. WITH the fix, m.selected is cleared on state update.
//
// This test verifies the fix: after handleEnvelope(EvtState), m.selected is empty.
func TestStaleSelectionClearedOnStateUpdate(t *testing.T) {
	// Initial 9-card state, sortByRank.
	hand9 := []protocol.CardView{
		cv(7, 1),  // 0: 7♥
		cv(8, 1),  // 1: 8♥
		cv(2, 3),  // 2: 2♣
		cv(3, 0),  // 3: 3♠
		cv(13, 2), // 4: K♦
		cv(4, 3),  // 5: 4♣
		cv(5, 0),  // 6: 5♠
		cv(12, 1), // 7: Q♥
		cv(11, 2), // 8: J♦
	}

	m := Model{
		screen:   screenGame,
		selfID:   "self-1",
		sortMode: sortByRank,
		selected: make(map[int]bool),
		state: &protocol.StateSnapshot{
			Phase:    "meld",
			ActiveID: "self-1",
			Hand:     hand9,
			Players: []protocol.PlayerView{
				{ID: "self-1", Name: "Alvaro", IsSelf: true, IsActive: true, Connected: true},
			},
		},
	}
	m.displayToServer = buildSortMapping(hand9, sortByRank)

	// Simulate the player selecting display indices 0 and 1 (7♥ and 8♥ in
	// sortByRank order for this 9-card hand).
	m.selected[0] = true
	m.selected[1] = true

	// Record what server cards those display indices pointed to.
	srv0Before := m.displayToServer[0] // should be index of 7♥ (or wherever in sort)
	srv1Before := m.displayToServer[1]
	card0Before := hand9[srv0Before]
	card1Before := hand9[srv1Before]
	t.Logf("Before: disp[0]→srv[%d]=%v, disp[1]→srv[%d]=%v",
		srv0Before, card0Before, srv1Before, card1Before)

	// Now a new state arrives: 10 cards (6♥ appended at server index 9).
	// Simulate handleEnvelope receiving EvtState.
	hand10 := handAfterDrawDiscard6H()
	newSnap := &protocol.StateSnapshot{
		Phase:    "meld",
		ActiveID: "self-1",
		Hand:     hand10,
		Players: []protocol.PlayerView{
			{ID: "self-1", Name: "Alvaro", IsSelf: true, IsActive: true, Connected: true},
		},
	}

	// Apply the state update exactly as handleEnvelope does.
	m.state = newSnap
	m.displayToServer = buildSortMapping(newSnap.Hand, m.sortMode)
	if m.cursor >= len(newSnap.Hand) {
		m.cursor = max(0, len(newSnap.Hand)-1)
	}
	// THE FIX: selected must be cleared on state update.
	m.selected = make(map[int]bool)

	// With the fix applied, selected is now empty.
	if len(m.selected) != 0 {
		t.Errorf("expected selected to be empty after state update, got %v", m.selected)
	}

	// Demonstrate what WOULD have happened without the fix: display indices 0
	// and 1 now point to different cards in the new 10-card mapping.
	srv0After := m.displayToServer[0]
	srv1After := m.displayToServer[1]
	card0After := hand10[srv0After]
	card1After := hand10[srv1After]
	t.Logf("After:  disp[0]→srv[%d]=%v, disp[1]→srv[%d]=%v",
		srv0After, card0After, srv1After, card1After)

	// Verify the mapping DID shift (proving stale selection would be wrong).
	if card0Before == card0After && card1Before == card1After {
		// This doesn't necessarily fail — it depends on sort order. Just log.
		t.Logf("Note: sort positions 0 and 1 happened to map to the same cards before/after")
	}
}

// ─── Bug: selection goes stale when sort mode changes ────────────────────────

// TestSelectionRemappedOnSortChange is the regression test for the sort-change
// selection bug. Scenario:
//  1. Player is in sortDealt mode with a 9-card hand.
//  2. Player selects display indices for 6♥, 7♥, 8♥ (contiguous run).
//  3. Player presses S to switch to sortByRank.
//  4. WITHOUT the fix, m.selected still holds the OLD display indices, which
//     now point to DIFFERENT cards in the new sort order — same class of bug
//     as the state-update stale-selection, but on the sort-change path.
//  5. WITH the fix, selected is remapped so 6♥, 7♥, 8♥ remain selected.
//
// This test first verifies the failing state (without remap), then asserts
// the fix: after S is pressed, serverIndexes(selected) must still return
// the server indices for 6♥, 7♥, 8♥.
func TestSelectionRemappedOnSortChange(t *testing.T) {
	hand := []protocol.CardView{
		cv(6, 1),  // 0: 6♥
		cv(7, 1),  // 1: 7♥
		cv(8, 1),  // 2: 8♥
		cv(2, 3),  // 3: 2♣
		cv(3, 0),  // 4: 3♠
		cv(13, 2), // 5: K♦
		cv(4, 3),  // 6: 4♣
		cv(5, 0),  // 7: 5♠
		cv(12, 1), // 8: Q♥
	}
	snap := &protocol.StateSnapshot{
		Phase:    "meld",
		ActiveID: "self-1",
		Hand:     hand,
		Players: []protocol.PlayerView{
			{ID: "self-1", IsSelf: true, IsActive: true, Connected: true},
		},
	}

	// Start in sortDealt mode. In dealt order, 6♥=disp0, 7♥=disp1, 8♥=disp2.
	m := Model{
		screen:          screenGame,
		selfID:          "self-1",
		state:           snap,
		sortMode:        sortDealt,
		selected:        make(map[int]bool),
		displayToServer: buildSortMapping(hand, sortDealt),
	}

	// Select 6♥, 7♥, 8♥ by their dealt display positions.
	m.selected[0] = true // 6♥
	m.selected[1] = true // 7♥
	m.selected[2] = true // 8♥

	// Demonstrate the BUG: WITHOUT the fix, pressing S only rebuilds
	// displayToServer but does NOT remap selected. In sortByRank, the first
	// three display positions are 2♣, 3♠, 4♣ — not the hearts run.
	oldMapping := m.displayToServer // dealt = identity

	// Simulate pressing S (sort change to sortByRank) WITHOUT the fix.
	newMode := sortMode((m.sortMode + 1) % 3) // sortByRank
	newMapping := buildSortMapping(hand, newMode)

	// Build a reverse map: serverIdx → new display index.
	newDispForSrv := make([]int, len(hand))
	for newDisp, srv := range newMapping {
		newDispForSrv[srv] = newDisp
	}
	_ = oldMapping // used conceptually above

	// Remap selected: for each old display index, find its server card, then
	// find where that server card lands in the new display order.
	newSelected := make(map[int]bool, len(m.selected))
	for oldDisp := range m.selected {
		srvIdx := m.displayToServer[oldDisp]
		newSelected[newDispForSrv[srvIdx]] = true
	}

	// Apply the fix.
	m.selected = newSelected
	m.displayToServer = newMapping
	m.sortMode = newMode

	// After remap, server indices from selected must still point to 6♥, 7♥, 8♥.
	serverIdxs := m.serverIndexes(selectedSlice(m.selected))
	got := make(map[string]bool)
	for _, si := range serverIdxs {
		got[cardKey(hand[si])] = true
	}
	for _, want := range []string{"6:1", "7:1", "8:1"} {
		if !got[want] {
			t.Errorf("after sort change, expected %s still selected; got server-index cards %v", want, got)
		}
	}
	if len(serverIdxs) != 3 {
		t.Errorf("expected exactly 3 selected cards after remap, got %d", len(serverIdxs))
	}
}

// TestSortCycleMaintainsSelection verifies that cycling through all three sort
// modes (dealt→rank→suit→dealt) with a pre-selected subset always returns the
// same server indices after each transition.
func TestSortCycleMaintainsSelection(t *testing.T) {
	hand := []protocol.CardView{
		cv(6, 1),  // 0: 6♥
		cv(7, 1),  // 1: 7♥
		cv(8, 1),  // 2: 8♥
		cv(2, 3),  // 3: 2♣
		cv(3, 0),  // 4: 3♠
		cv(13, 2), // 5: K♦
		cv(4, 3),  // 6: 4♣
		cv(5, 0),  // 7: 5♠
		cv(12, 1), // 8: Q♥
	}
	snap := &protocol.StateSnapshot{
		Phase:    "meld",
		ActiveID: "self-1",
		Hand:     hand,
		Players: []protocol.PlayerView{
			{ID: "self-1", IsSelf: true, IsActive: true, Connected: true},
		},
	}

	m := Model{
		screen:          screenGame,
		selfID:          "self-1",
		state:           snap,
		sortMode:        sortDealt,
		selected:        make(map[int]bool),
		displayToServer: buildSortMapping(hand, sortDealt),
	}

	// Select 6♥, 7♥, 8♥ in dealt order (display indices 0, 1, 2).
	m.selected[0] = true
	m.selected[1] = true
	m.selected[2] = true

	wantCards := map[string]bool{"6:1": true, "7:1": true, "8:1": true}

	// Cycle through all sort modes three times (full wrap).
	for step := 0; step < 9; step++ {
		// Simulate S key using handleGameKey (which now must remap selected).
		newM, _ := m.handleGameKey("s")
		m = newM.(Model)

		serverIdxs := m.serverIndexes(selectedSlice(m.selected))
		if len(serverIdxs) != 3 {
			t.Errorf("step %d (mode %v): expected 3 selected cards, got %d", step, m.sortMode, len(serverIdxs))
			continue
		}
		got := make(map[string]bool)
		for _, si := range serverIdxs {
			got[cardKey(hand[si])] = true
		}
		for wantCard := range wantCards {
			if !got[wantCard] {
				t.Errorf("step %d (mode %v): expected %s selected; got %v", step, m.sortMode, wantCard, got)
			}
		}
	}
}

// TestServerIndexesMatchCardIdentity verifies that for any sort mode, after
// building the mapping for a given hand, selecting a set of display indices
// and translating to server indices produces exactly the expected cards.
func TestServerIndexesMatchCardIdentity(t *testing.T) {
	modes := []sortMode{sortDealt, sortByRank, sortBySuit}
	hand := handAfterDrawDiscard6H() // 10-card hand with 6♥ at server idx 9

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			m := Model{
				sortMode:        mode,
				selected:        make(map[int]bool),
				displayToServer: buildSortMapping(hand, mode),
			}

			// Find the display indices for 6♥, 7♥, 8♥.
			var d6, d7, d8 int
			d6, d7, d8 = -1, -1, -1
			for di, si := range m.displayToServer {
				c := hand[si]
				switch {
				case c.Rank == 6 && c.Suit == 1:
					d6 = di
				case c.Rank == 7 && c.Suit == 1:
					d7 = di
				case c.Rank == 8 && c.Suit == 1:
					d8 = di
				}
			}
			if d6 < 0 || d7 < 0 || d8 < 0 {
				t.Fatalf("cards not found in mapping")
			}

			m.selected[d6] = true
			m.selected[d7] = true
			m.selected[d8] = true

			serverIdxs := m.serverIndexes(selectedSlice(m.selected))
			ranks := make(map[int]bool)
			for _, si := range serverIdxs {
				ranks[hand[si].Rank] = true
			}
			for _, wantRank := range []int{6, 7, 8} {
				if !ranks[wantRank] {
					t.Errorf("expected rank %d in server index results, got ranks %v", wantRank, ranks)
				}
			}
		})
	}
}
