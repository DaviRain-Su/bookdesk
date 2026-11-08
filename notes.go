package main

import "context"

// Notes 负责划线与卡片。
type Notes struct{}

func (Notes) SaveHighlight(ctx context.Context, input SaveHighlightInput) (Highlight, error) {
	if input.ID == nil && input.Location.CFI != nil {
		existing, err := findHighlightByCFI(input.BookID, *input.Location.CFI)
		if err != nil {
			return Highlight{}, err
		}
		if existing != nil {
			return *existing, nil
		}
	}
	id := newID("hl")
	if input.ID != nil {
		id = *input.ID
	}
	color := "yellow"
	if input.Color != nil {
		color = *input.Color
	}
	h := Highlight{
		ID:        id,
		BookID:    input.BookID,
		Location:  input.Location,
		Text:      input.Text,
		Color:     color,
		CreatedAt: nowSeconds(),
	}
	if err := insertHighlight(h); err != nil {
		return Highlight{}, err
	}
	return h, nil
}

func (Notes) ListHighlights(ctx context.Context, bookID *string) ([]Highlight, error) {
	return listHighlights(bookID)
}

func (Notes) DeleteHighlight(ctx context.Context, id string) error {
	return deleteHighlight(id)
}

func (Notes) SaveCard(ctx context.Context, input SaveCardInput) (Card, error) {
	id := newID("card")
	if input.ID != nil {
		id = *input.ID
	}
	now := nowSeconds()
	card := Card{
		ID:            id,
		Type:          input.Type,
		Title:         input.Title,
		Body:          input.Body,
		Quote:         input.Quote,
		QuoteLocation: input.QuoteLocation,
		ImageRelPath:  input.ImageRelPath,
		SourceBookID:  input.SourceBookID,
		CharacterName: input.CharacterName,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := upsertCard(card); err != nil {
		return Card{}, err
	}
	return card, nil
}

func (Notes) ListCards(ctx context.Context) ([]Card, error) {
	return listCards()
}

func (Notes) DeleteCard(ctx context.Context, id string) error {
	return deleteCard(id)
}
