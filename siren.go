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
		subEntity := SirenSubEntity{
			Class: []string{"event", "note"},
			Rel:   []string{"item"},
			Properties: map[string]interface{}{
				"id":          item.ID,
				"kind":        item.Kind,
				"pubkey":      item.Pubkey,
				"created_at":  item.CreatedAt,
				"content":     item.Content,
				"tags":        item.Tags,
				"sig":         item.Sig,
				"relays_seen": item.RelaysSeen,
			},
			Links: []SirenLink{
				{
					Rel:  []string{"author"},
					Href: "/profiles/" + item.Pubkey,
				},
			},
			Actions: []SirenAction{
				{
					Name:   "react",
					Title:  "React to this event",
					Method: "POST",
					Href:   "/actions/react",
					Type:   "application/json",
					Fields: []SirenField{
						{Name: "event_id", Type: "text", Value: item.ID},
						{Name: "reaction", Type: "text", Value: "+"},
					},
				},
			},
		}

		// Add thread link if this is a reply
		for _, tag := range item.Tags {
			if len(tag) >= 2 && tag[0] == "e" {
				subEntity.Links = append(subEntity.Links, SirenLink{
					Rel:  []string{"thread"},
					Href: "/threads/" + tag[1],
				})
				break
			}
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

	// Add publish action
	entity.Actions = append(entity.Actions, SirenAction{
		Name:   "publish",
		Title:  "Publish a new note",
		Method: "POST",
		Href:   "/events",
		Type:   "application/json",
		Fields: []SirenField{
			{Name: "content", Type: "text", Title: "Note content"},
			{Name: "tags", Type: "text", Title: "Tags (optional JSON array)"},
		},
	})

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
