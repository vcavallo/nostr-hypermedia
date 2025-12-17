package main

import (
	"strconv"
	"strings"
)

type SirenEntity struct {
	Class      []string                 `json:"class,omitempty"`
	Properties map[string]interface{}   `json:"properties,omitempty"`
	Entities   []SirenSubEntity         `json:"entities,omitempty"`
	Links      []SirenLink              `json:"links,omitempty"`
	Actions    []SirenAction            `json:"actions,omitempty"`
}

type SirenSubEntity struct {
	Class      []string               `json:"class,omitempty"`
	Rel        []string               `json:"rel,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	Links      []SirenLink            `json:"links,omitempty"`
	Actions    []SirenAction          `json:"actions,omitempty"`
}

type SirenLink struct {
	Rel  []string `json:"rel"`
	Href string   `json:"href"`
}

type SirenAction struct {
	Name   string        `json:"name"`
	Title  string        `json:"title,omitempty"`
	Method string        `json:"method"`
	Href   string        `json:"href"`
	Type   string        `json:"type,omitempty"`
	Fields []SirenField  `json:"fields,omitempty"`
}

type SirenField struct {
	Name  string      `json:"name"`
	Type  string      `json:"type,omitempty"`
	Value interface{} `json:"value,omitempty"`
	Title string      `json:"title,omitempty"`
}

func toSirenTimeline(resp TimelineResponse, relays []string, authors []string, kinds []int, limit int) SirenEntity {
	// Build main entity
	entity := SirenEntity{
		Class: []string{"timeline"},
		Properties: map[string]interface{}{
			"title":           "Nostr Timeline",
			"queried_relays":  resp.Meta.QueriedRelays,
			"eose":            resp.Meta.EOSE,
			"generated_at":    resp.Meta.GeneratedAt,
		},
		Entities: []SirenSubEntity{},
		Links:    []SirenLink{},
		Actions:  []SirenAction{},
	}

	// Add event entities
	for _, item := range resp.Items {
		props := map[string]interface{}{
			"id":          item.ID,
			"kind":        item.Kind,
			"pubkey":      item.Pubkey,
			"created_at":  item.CreatedAt,
			"content":     item.Content,
			"tags":        item.Tags,
			"sig":         item.Sig,
			"relays_seen": item.RelaysSeen,
		}

		// Add author profile if available
		if item.AuthorProfile != nil {
			props["author_profile"] = map[string]interface{}{
				"name":         item.AuthorProfile.Name,
				"display_name": item.AuthorProfile.DisplayName,
				"picture":      item.AuthorProfile.Picture,
				"nip05":        item.AuthorProfile.Nip05,
			}
		}

		// Add reactions if available
		if item.Reactions != nil {
			props["reactions"] = map[string]interface{}{
				"total":   item.Reactions.Total,
				"by_type": item.Reactions.ByType,
			}
		}

		// Add reply count
		props["reply_count"] = item.ReplyCount

		// Build actions for this event using unified action system
		// For Siren API, we show all available actions (auth required on submission)
		ctx := ActionContext{
			EventID:      item.ID,
			EventPubkey:  item.Pubkey,
			Kind:         item.Kind,
			ReplyCount:   item.ReplyCount,
			LoggedIn:     true, // Show all actions in API
			HasWallet:    true, // Show zap in API (consumer checks wallet status)
			CSRFToken:    "",   // Not needed for API
			ReturnURL:    "",
			LoginURL:     "",
		}
		hypermedia := BuildHypermediaEntity(ctx, item.Tags, nil)
		sirenActions := make([]SirenAction, len(hypermedia.Actions))
		for i, def := range hypermedia.Actions {
			sirenActions[i] = def.ToSirenAction()
		}

		subEntity := SirenSubEntity{
			Class:      []string{"event", "note"},
			Rel:        []string{"item"},
			Properties: props,
			Links:      []SirenLink{},
			Actions:    sirenActions,
		}

		entity.Entities = append(entity.Entities, subEntity)
	}

	// Add self link
	selfURL := buildTimelineURL("/timeline", relays, authors, kinds, limit, nil)
	entity.Links = append(entity.Links, SirenLink{
		Rel:  []string{"self"},
		Href: selfURL,
	})

	// Add next link for pagination
	if resp.Page.Next != nil {
		entity.Links = append(entity.Links, SirenLink{
			Rel:  []string{"next"},
			Href: *resp.Page.Next,
		})
	}

	return entity
}

func buildTimelineURL(base string, relays []string, authors []string, kinds []int, limit int, until *int64) string {
	parts := []string{base + "?"}

	if len(relays) > 0 {
		parts = append(parts, "relays="+strings.Join(relays, ","))
	}
	if len(authors) > 0 {
		parts = append(parts, "authors="+strings.Join(authors, ","))
	}
	if len(kinds) > 0 {
		kindsStr := make([]string, len(kinds))
		for i, k := range kinds {
			kindsStr[i] = strconv.Itoa(k)
		}
		parts = append(parts, "kinds="+strings.Join(kindsStr, ","))
	}
	parts = append(parts, "limit="+strconv.Itoa(limit))
	if until != nil {
		parts = append(parts, "until="+strconv.FormatInt(*until, 10))
	}

	return strings.Join(parts, "&")
}
