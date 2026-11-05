package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is satisfied by *pgxpool.Pool and *pgx.Conn.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Store reads and changes the catalog through the gateway (gk_api login).
// Staff methods take the actor's verified provider subject; the database
// checks its capability again.
type Store struct{ DB Querier }

// Servings is an inclusive range.
type Servings struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Notice is a shared-kitchen allergen notice version.
type Notice struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at,omitzero"`
}

// Recipe is the purchasable content of a variant's current recipe version.
type Recipe struct {
	ID          string     `json:"recipe_version_id"`
	Revision    int        `json:"revision"`
	PriceMinor  int64      `json:"price_minor"`
	Currency    string     `json:"currency"`
	Servings    Servings   `json:"servings"`
	Coating     string     `json:"coating"`
	Composition []string   `json:"composition"`
	Allergens   []Allergen `json:"allergens"`
	Notice      Notice     `json:"allergen_notice"`
}

// Variant is one flavour in one size. Recipe is nil when the chef has not
// published a recipe version yet: the variant is then not purchasable
// (enquiry only).
type Variant struct {
	ID          string     `json:"id"`
	Flavour     string     `json:"flavour"`
	Size        Size       `json:"size"`
	StockMode   string     `json:"stock_mode"`
	Purchasable bool       `json:"purchasable"`
	Recipe      *Recipe    `json:"recipe"`
	Version     int        `json:"version,omitempty"`     // staff only
	ArchivedAt  *time.Time `json:"archived_at,omitempty"` // staff only
}

// CakeColour is a cake's colour. Customers see only an active colour's
// code, label and position (an archived colour reads as no colour); staff
// also see its ID and archive time.
type CakeColour struct {
	ID         string     `json:"id,omitempty"` // staff only
	Code       string     `json:"code"`
	Label      string     `json:"label"`
	Position   int        `json:"position"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"` // staff only
}

// Colour is one entry of the chef's colour list.
type Colour struct {
	ID         string     `json:"id"`
	Code       string     `json:"code"`
	Label      string     `json:"label"`
	Position   int        `json:"position"`
	ArchivedAt *time.Time `json:"archived_at"`
	Version    int        `json:"version"`
}

// Cake is a listing with its variants. State, Version and the timestamps
// other than PublishedAt are staff only. Colour is always present (null
// when the cake has none).
type Cake struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Colour      *CakeColour `json:"colour"`
	PublishedAt *time.Time  `json:"published_at,omitempty"`
	State       string      `json:"state,omitempty"`
	CreatedAt   *time.Time  `json:"created_at,omitempty"`
	UpdatedAt   *time.Time  `json:"updated_at,omitempty"`
	Version     int         `json:"version,omitempty"`
	Variants    []Variant   `json:"variants"`
}

// Page is one page of cakes; NextCursor is empty on the last page.
type Page struct {
	Cakes      []Cake `json:"cakes"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// RecipeVersion is one immutable version: what an order pins.
type RecipeVersion struct {
	Recipe
	VariantID    string    `json:"variant_id"`
	CakeID       string    `json:"cake_id"`
	Flavour      string    `json:"flavour"`
	Size         Size      `json:"size"`
	Lines        []Line    `json:"lines"`
	SourceSHA256 string    `json:"source_sha256"`
	CreatedAt    time.Time `json:"created_at"`
}

// Flavour is an active flavour.
type Flavour struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Result is a command's outcome: the affected object and whether the
// command was a replay of an earlier one with the same key.
type Result struct {
	ID       string
	Replayed bool
}

// --------------------------------------------------------------- cursor

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(c string) (*time.Time, *string, error) {
	if c == "" {
		return nil, nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || len(raw) > 100 {
		return nil, nil, ErrBadCursor
	}
	ts, id, ok := strings.Cut(string(raw), "|")
	if !ok || !ValidUUID(id) {
		return nil, nil, ErrBadCursor
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return nil, nil, ErrBadCursor
	}
	return &t, &id, nil
}

func pageSize(limit int) int {
	switch {
	case limit <= 0:
		return DefaultPageSize
	case limit > MaxPageSize:
		return MaxPageSize
	}
	return limit
}

// ---------------------------------------------------------------- reads

// PublicPage returns active cakes, newest first.
func (s *Store) PublicPage(ctx context.Context, cursor string, limit int) (Page, error) {
	after, afterID, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	n := pageSize(limit)
	rows, err := s.DB.Query(ctx, `SELECT cake_id::text, cake_name, cake_description, cake_published_at
		FROM business.catalog_public_cake_page($1, $2::uuid, $3)`, after, afterID, n+1)
	if err != nil {
		return Page{}, fmt.Errorf("catalog: public page: %w", err)
	}
	cakes, err := pgx.CollectRows(rows, scanPublicCake)
	if err != nil {
		return Page{}, fmt.Errorf("catalog: public page: %w", err)
	}
	page := Page{Cakes: cakes}
	if len(cakes) > n {
		page.Cakes = cakes[:n]
		last := page.Cakes[n-1]
		page.NextCursor = encodeCursor(*last.PublishedAt, last.ID)
	}
	if err := s.attachVariants(ctx, page.Cakes, `SELECT `+variantColumns+` FROM business.catalog_public_variants($1::uuid[])`, false); err != nil {
		return Page{}, err
	}
	if err := s.attachPublicColours(ctx, page.Cakes); err != nil {
		return Page{}, err
	}
	return page, nil
}

// PublicCake returns an active cake, or ErrNotFound (missing, draft or
// archived).
func (s *Store) PublicCake(ctx context.Context, id string) (Cake, error) {
	if !ValidUUID(id) {
		return Cake{}, ErrNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT cake_id::text, cake_name, cake_description, cake_published_at
		FROM business.catalog_public_cake($1::uuid)`, id)
	if err != nil {
		return Cake{}, fmt.Errorf("catalog: public cake: %w", err)
	}
	cakes, err := pgx.CollectRows(rows, scanPublicCake)
	if err != nil {
		return Cake{}, fmt.Errorf("catalog: public cake: %w", err)
	}
	if len(cakes) == 0 {
		return Cake{}, ErrNotFound
	}
	if err := s.attachVariants(ctx, cakes, `SELECT `+variantColumns+` FROM business.catalog_public_variants($1::uuid[])`, false); err != nil {
		return Cake{}, err
	}
	if err := s.attachPublicColours(ctx, cakes); err != nil {
		return Cake{}, err
	}
	return cakes[0], nil
}

// StaffPage returns cakes in every state, newest created first.
func (s *Store) StaffPage(ctx context.Context, subject, cursor string, limit int) (Page, error) {
	after, afterID, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	n := pageSize(limit)
	rows, err := s.DB.Query(ctx, `SELECT `+staffCakeColumns+`
		FROM business.catalog_staff_cake_page($1::uuid, $2, $3::uuid, $4)`, subject, after, afterID, n+1)
	if err != nil {
		return Page{}, gatewayError("staff page", err)
	}
	cakes, err := pgx.CollectRows(rows, scanStaffCake)
	if err != nil {
		return Page{}, gatewayError("staff page", err)
	}
	page := Page{Cakes: cakes}
	if len(cakes) > n {
		page.Cakes = cakes[:n]
		last := page.Cakes[n-1]
		page.NextCursor = encodeCursor(*last.CreatedAt, last.ID)
	}
	if err := s.attachStaffVariants(ctx, subject, page.Cakes); err != nil {
		return Page{}, err
	}
	if err := s.attachStaffColours(ctx, subject, page.Cakes); err != nil {
		return Page{}, err
	}
	return page, nil
}

// StaffCake returns a cake in any state with all its variants.
func (s *Store) StaffCake(ctx context.Context, subject, id string) (Cake, error) {
	if !ValidUUID(id) {
		return Cake{}, ErrNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT `+staffCakeColumns+` FROM business.catalog_staff_cake($1::uuid, $2::uuid)`, subject, id)
	if err != nil {
		return Cake{}, gatewayError("staff cake", err)
	}
	cakes, err := pgx.CollectRows(rows, scanStaffCake)
	if err != nil {
		return Cake{}, gatewayError("staff cake", err)
	}
	if len(cakes) == 0 {
		return Cake{}, ErrNotFound
	}
	if err := s.attachStaffVariants(ctx, subject, cakes); err != nil {
		return Cake{}, err
	}
	if err := s.attachStaffColours(ctx, subject, cakes); err != nil {
		return Cake{}, err
	}
	return cakes[0], nil
}

// RecipeVersion returns one immutable recipe version for staff.
func (s *Store) RecipeVersion(ctx context.Context, subject, id string) (RecipeVersion, error) {
	if !ValidUUID(id) {
		return RecipeVersion{}, ErrNotFound
	}
	rows, err := s.DB.Query(ctx, `SELECT recipe_version_id::text, recipe_variant_id::text, recipe_cake_id::text,
		recipe_flavour, recipe_size_code, recipe_revision, recipe_price_minor, recipe_servings_min,
		recipe_servings_max, recipe_coating, recipe_composition, recipe_allergens, recipe_lines,
		recipe_notice_id::text, recipe_notice, recipe_source_sha256, recipe_created_at
		FROM business.catalog_staff_recipe_version($1::uuid, $2::uuid)`, subject, id)
	if err != nil {
		return RecipeVersion{}, gatewayError("recipe version", err)
	}
	versions, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RecipeVersion, error) {
		var v RecipeVersion
		var size string
		var allergens, lines []byte
		err := r.Scan(&v.ID, &v.VariantID, &v.CakeID, &v.Flavour, &size, &v.Revision, &v.PriceMinor,
			&v.Servings.Min, &v.Servings.Max, &v.Coating, &v.Composition, &allergens, &lines,
			&v.Notice.ID, &v.Notice.Text, &v.SourceSHA256, &v.CreatedAt)
		if err != nil {
			return v, err
		}
		v.Currency = Currency
		v.Size, _ = SizeByCode(size)
		if err := json.Unmarshal(allergens, &v.Allergens); err != nil {
			return v, err
		}
		return v, json.Unmarshal(lines, &v.Lines)
	})
	if err != nil {
		return RecipeVersion{}, gatewayError("recipe version", err)
	}
	if len(versions) == 0 {
		return RecipeVersion{}, ErrNotFound
	}
	return versions[0], nil
}

// Flavours lists the active flavours by name.
func (s *Store) Flavours(ctx context.Context, subject string) ([]Flavour, error) {
	rows, err := s.DB.Query(ctx, `SELECT flavour_id::text, flavour_name, flavour_created_at
		FROM business.catalog_staff_flavours($1::uuid)`, subject)
	if err != nil {
		return nil, gatewayError("flavours", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Flavour, error) {
		var f Flavour
		return f, r.Scan(&f.ID, &f.Name, &f.CreatedAt)
	})
	if err != nil {
		return nil, gatewayError("flavours", err)
	}
	if list == nil {
		list = []Flavour{}
	}
	return list, nil
}

// CurrentNotice returns the newest allergen notice, or ErrNotFound when the
// chef has not recorded one yet.
func (s *Store) CurrentNotice(ctx context.Context, subject string) (Notice, error) {
	rows, err := s.DB.Query(ctx, `SELECT notice_id::text, notice_body, notice_created_at
		FROM business.catalog_staff_notice_current($1::uuid)`, subject)
	if err != nil {
		return Notice{}, gatewayError("notice", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) {
		var n Notice
		return n, r.Scan(&n.ID, &n.Text, &n.CreatedAt)
	})
	if err != nil {
		return Notice{}, gatewayError("notice", err)
	}
	if len(list) == 0 {
		return Notice{}, ErrNotFound
	}
	return list[0], nil
}

// Colours lists every colour, archived ones included, in the filter order
// (position, then label).
func (s *Store) Colours(ctx context.Context, subject string) ([]Colour, error) {
	return s.colours(ctx, subject, nil)
}

// Colour returns one colour, or ErrNotFound.
func (s *Store) Colour(ctx context.Context, subject, id string) (Colour, error) {
	if !ValidUUID(id) {
		return Colour{}, ErrNotFound
	}
	list, err := s.colours(ctx, subject, &id)
	if err != nil {
		return Colour{}, err
	}
	if len(list) == 0 {
		return Colour{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) colours(ctx context.Context, subject string, id *string) ([]Colour, error) {
	rows, err := s.DB.Query(ctx, `SELECT colour_id::text, colour_code, colour_label, colour_position, colour_archived_at,
		colour_version FROM business.catalog_staff_colours($1::uuid, $2::uuid)`, subject, id)
	if err != nil {
		return nil, gatewayError("colours", err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Colour, error) {
		var c Colour
		return c, r.Scan(&c.ID, &c.Code, &c.Label, &c.Position, &c.ArchivedAt, &c.Version)
	})
	if err != nil {
		return nil, gatewayError("colours", err)
	}
	if list == nil {
		list = []Colour{}
	}
	return list, nil
}

func cakeIDs(cakes []Cake) []string {
	ids := make([]string, len(cakes))
	for i, c := range cakes {
		ids[i] = c.ID
	}
	return ids
}

// attachPublicColours sets each active cake's active colour; the rest stay
// nil (no colour).
func (s *Store) attachPublicColours(ctx context.Context, cakes []Cake) error {
	if len(cakes) == 0 {
		return nil
	}
	rows, err := s.DB.Query(ctx, `SELECT cake_id::text, colour_code, colour_label, colour_position
		FROM business.catalog_public_cake_colours($1::uuid[])`, cakeIDs(cakes))
	if err != nil {
		return fmt.Errorf("catalog: colours: %w", err)
	}
	return collectColours(rows, cakes, func(r pgx.Rows, cakeID *string, c *CakeColour) error {
		return r.Scan(cakeID, &c.Code, &c.Label, &c.Position)
	})
}

func (s *Store) attachStaffColours(ctx context.Context, subject string, cakes []Cake) error {
	if len(cakes) == 0 {
		return nil
	}
	rows, err := s.DB.Query(ctx, `SELECT cake_id::text, colour_id::text, colour_code, colour_label, colour_position,
		colour_archived_at FROM business.catalog_staff_cake_colours($1::uuid, $2::uuid[])`, subject, cakeIDs(cakes))
	if err != nil {
		return gatewayError("staff colours", err)
	}
	return collectColours(rows, cakes, func(r pgx.Rows, cakeID *string, c *CakeColour) error {
		return r.Scan(cakeID, &c.ID, &c.Code, &c.Label, &c.Position, &c.ArchivedAt)
	})
}

func collectColours(rows pgx.Rows, cakes []Cake, scan func(pgx.Rows, *string, *CakeColour) error) error {
	index := make(map[string]int, len(cakes))
	for i, c := range cakes {
		index[c.ID] = i
	}
	defer rows.Close()
	for rows.Next() {
		var cakeID string
		c := &CakeColour{}
		if err := scan(rows, &cakeID, c); err != nil {
			return gatewayError("colours", err)
		}
		if i, ok := index[cakeID]; ok {
			cakes[i].Colour = c
		}
	}
	if err := rows.Err(); err != nil {
		return gatewayError("colours", err)
	}
	return nil
}

const staffCakeColumns = `cake_id::text, cake_name, cake_description, cake_state, cake_published_at,
	cake_created_at, cake_updated_at, cake_version`

const variantColumns = `variant_id::text, variant_cake_id::text, variant_flavour, variant_size_code,
	variant_stock_mode, variant_archived_at, variant_version, recipe_version_id::text, recipe_revision,
	recipe_price_minor, recipe_servings_min, recipe_servings_max, recipe_coating, recipe_composition,
	recipe_allergens, recipe_notice_id::text, recipe_notice`

func scanPublicCake(r pgx.CollectableRow) (Cake, error) {
	var c Cake
	var published time.Time
	err := r.Scan(&c.ID, &c.Name, &c.Description, &published)
	c.PublishedAt = &published
	c.Variants = []Variant{}
	return c, err
}

func scanStaffCake(r pgx.CollectableRow) (Cake, error) {
	var c Cake
	var created, updated time.Time
	err := r.Scan(&c.ID, &c.Name, &c.Description, &c.State, &c.PublishedAt, &created, &updated, &c.Version)
	c.CreatedAt, c.UpdatedAt = &created, &updated
	c.Variants = []Variant{}
	return c, err
}

func (s *Store) attachStaffVariants(ctx context.Context, subject string, cakes []Cake) error {
	if len(cakes) == 0 {
		return nil
	}
	ids := make([]string, len(cakes))
	for i, c := range cakes {
		ids[i] = c.ID
	}
	rows, err := s.DB.Query(ctx, `SELECT `+variantColumns+` FROM business.catalog_staff_variants($1::uuid, $2::uuid[])`, subject, ids)
	if err != nil {
		return gatewayError("staff variants", err)
	}
	return collectVariants(rows, cakes, true)
}

func (s *Store) attachVariants(ctx context.Context, cakes []Cake, sql string, staff bool) error {
	if len(cakes) == 0 {
		return nil
	}
	ids := make([]string, len(cakes))
	for i, c := range cakes {
		ids[i] = c.ID
	}
	rows, err := s.DB.Query(ctx, sql, ids)
	if err != nil {
		return fmt.Errorf("catalog: variants: %w", err)
	}
	return collectVariants(rows, cakes, staff)
}

func collectVariants(rows pgx.Rows, cakes []Cake, staff bool) error {
	index := make(map[string]int, len(cakes))
	for i, c := range cakes {
		index[c.ID] = i
	}
	defer rows.Close()
	for rows.Next() {
		var (
			v                                   Variant
			cakeID, size                        string
			archived                            *time.Time
			version                             int
			recipeID, noticeID, notice, coating *string
			revision, servingsMin, servingsMax  *int
			price                               *int64
			composition                         []string
			allergens                           []byte
		)
		if err := rows.Scan(&v.ID, &cakeID, &v.Flavour, &size, &v.StockMode, &archived, &version, &recipeID,
			&revision, &price, &servingsMin, &servingsMax, &coating, &composition, &allergens, &noticeID, &notice); err != nil {
			return gatewayError("variants", err)
		}
		v.Size, _ = SizeByCode(size)
		if recipeID != nil {
			r := &Recipe{ID: *recipeID, Revision: *revision, PriceMinor: *price, Currency: Currency,
				Servings: Servings{Min: *servingsMin, Max: *servingsMax}, Coating: *coating,
				Composition: composition, Notice: Notice{ID: *noticeID, Text: *notice}}
			if err := json.Unmarshal(allergens, &r.Allergens); err != nil {
				return fmt.Errorf("catalog: variants: allergens: %w", err)
			}
			v.Recipe = r
		}
		// K01: missing allergen disclosure disables buying. A version always
		// declares allergens and pins a notice; check both anyway.
		v.Purchasable = archived == nil && v.Recipe != nil && len(v.Recipe.Allergens) > 0 && v.Recipe.Notice.Text != ""
		if staff {
			v.Version, v.ArchivedAt = version, archived
		}
		if i, ok := index[cakeID]; ok {
			cakes[i].Variants = append(cakes[i].Variants, v)
		}
	}
	if err := rows.Err(); err != nil {
		return gatewayError("variants", err)
	}
	return nil
}

// -------------------------------------------------------------- commands

// CreateFlavour creates a flavour.
func (s *Store) CreateFlavour(ctx context.Context, subject, key string, in FlavourInput) (Result, error) {
	return s.command(ctx, CmdFlavourCreate, `SELECT result_id::text, replayed
		FROM business.catalog_flavour_create($1::uuid, $2, $3, $4)`,
		subject, key, BodyHash(CmdFlavourCreate, "", in), in.Name)
}

// CreateNotice records a new allergen notice version.
func (s *Store) CreateNotice(ctx context.Context, subject, key string, in NoticeInput) (Result, error) {
	return s.command(ctx, CmdNoticeCreate, `SELECT result_id::text, replayed
		FROM business.catalog_notice_create($1::uuid, $2, $3, $4)`,
		subject, key, BodyHash(CmdNoticeCreate, "", in), in.Body)
}

// CreateCake creates a draft cake, with a colour when ColourID is set. A
// cake without a colour goes through the 0009 function (same command
// identity and effect), so it needs no later schema.
func (s *Store) CreateCake(ctx context.Context, subject, key string, in CakeInput) (Result, error) {
	hash := BodyHash(CmdCakeCreate, "", in)
	if in.ColourID == nil {
		return s.command(ctx, CmdCakeCreate, `SELECT result_id::text, replayed
			FROM business.catalog_cake_create($1::uuid, $2, $3, $4, $5)`,
			subject, key, hash, in.Name, in.Description)
	}
	return s.command(ctx, CmdCakeCreate, `SELECT result_id::text, replayed
		FROM business.catalog_cake_create($1::uuid, $2, $3, $4, $5, $6::uuid)`,
		subject, key, hash, in.Name, in.Description, *in.ColourID)
}

// UpdateCake changes a cake's name, description and/or colour.
func (s *Store) UpdateCake(ctx context.Context, subject, key, cakeID string, in CakePatch) (Result, error) {
	if !ValidUUID(cakeID) {
		return Result{}, ErrNotFound
	}
	var name, description *string
	if in.Name.Set {
		name = &in.Name.Value
	}
	if in.Description.Set {
		description = &in.Description.Value
	}
	hash := BodyHash(CmdCakeUpdate, cakeID, in)
	if !in.ColourID.Set {
		// No colour change: the 0009 function leaves the colour as it is.
		return s.command(ctx, CmdCakeUpdate, `SELECT result_id::text, replayed
			FROM business.catalog_cake_update($1::uuid, $2, $3, $4::uuid, $5, $6, $7)`,
			subject, key, hash, cakeID, in.ExpectedVersion, name, description)
	}
	var colour *string
	if !in.ColourID.Null {
		colour = &in.ColourID.Value
	}
	return s.command(ctx, CmdCakeUpdate, `SELECT result_id::text, replayed
		FROM business.catalog_cake_update($1::uuid, $2, $3, $4::uuid, $5, $6, $7, true, $8::uuid)`,
		subject, key, hash, cakeID, in.ExpectedVersion, name, description, colour)
}

// CreateColour adds a colour to the chef's list.
func (s *Store) CreateColour(ctx context.Context, subject, key string, in ColourInput) (Result, error) {
	return s.command(ctx, CmdColourCreate, `SELECT result_id::text, replayed
		FROM business.catalog_colour_create($1::uuid, $2, $3, $4, $5, $6)`,
		subject, key, BodyHash(CmdColourCreate, "", in), in.Code, in.Label, in.Position)
}

// UpdateColour changes a colour's label or position, or archives or
// restores it.
func (s *Store) UpdateColour(ctx context.Context, subject, key, colourID string, in ColourPatch) (Result, error) {
	if !ValidUUID(colourID) {
		return Result{}, ErrNotFound
	}
	var label *string
	var position *int
	var archived *bool
	if in.Label.Set {
		label = &in.Label.Value
	}
	if in.Position.Set {
		position = &in.Position.Value
	}
	if in.Archived.Set {
		archived = &in.Archived.Value
	}
	return s.command(ctx, CmdColourUpdate, `SELECT result_id::text, replayed
		FROM business.catalog_colour_update($1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8)`,
		subject, key, BodyHash(CmdColourUpdate, colourID, in), colourID, in.ExpectedVersion, label, position, archived)
}

// SetCakeState publishes, archives or restores a cake.
func (s *Store) SetCakeState(ctx context.Context, subject, key, cakeID string, in CakeStateInput) (Result, error) {
	if !ValidUUID(cakeID) {
		return Result{}, ErrNotFound
	}
	return s.command(ctx, CmdCakeSetState, `SELECT result_id::text, replayed
		FROM business.catalog_cake_set_state($1::uuid, $2, $3, $4::uuid, $5, $6)`,
		subject, key, BodyHash(CmdCakeSetState, cakeID, in), cakeID, in.ExpectedVersion, in.State)
}

// CreateVariant adds a variant to a cake.
func (s *Store) CreateVariant(ctx context.Context, subject, key, cakeID string, in VariantInput) (Result, error) {
	if !ValidUUID(cakeID) {
		return Result{}, ErrNotFound
	}
	return s.command(ctx, CmdVariantCreate, `SELECT result_id::text, replayed
		FROM business.catalog_variant_create($1::uuid, $2, $3, $4::uuid, $5::uuid, $6, $7)`,
		subject, key, BodyHash(CmdVariantCreate, cakeID, in), cakeID, in.FlavourID, in.SizeCode, in.StockMode)
}

// ArchiveVariant archives a variant; its recipe versions stay.
func (s *Store) ArchiveVariant(ctx context.Context, subject, key, variantID string, in VersionInput) (Result, error) {
	if !ValidUUID(variantID) {
		return Result{}, ErrNotFound
	}
	return s.command(ctx, CmdVariantArchive, `SELECT result_id::text, replayed
		FROM business.catalog_variant_archive($1::uuid, $2, $3, $4::uuid, $5)`,
		subject, key, BodyHash(CmdVariantArchive, variantID, in), variantID, in.ExpectedVersion)
}

// CreateRecipeVersion creates the next immutable recipe version of a variant
// and makes it current.
func (s *Store) CreateRecipeVersion(ctx context.Context, subject, key string, in RecipeVersionInput) (Result, error) {
	allergens, err := json.Marshal(in.Allergens)
	if err != nil {
		return Result{}, err
	}
	lines := in.Lines
	if lines == nil {
		lines = []Line{}
	}
	linesJSON, err := json.Marshal(lines)
	if err != nil {
		return Result{}, err
	}
	return s.command(ctx, CmdRecipeVersionCreate, `SELECT result_id::text, replayed
		FROM business.catalog_recipe_version_create($1::uuid, $2, $3, $4::uuid, $5, $6, $7, $8, $9, $10, $11::uuid,
		  $12::json, $13::json, $14)`,
		subject, key, BodyHash(CmdRecipeVersionCreate, in.VariantID, in.normalized()), in.VariantID, in.ExpectedVersion, in.PriceMinor,
		in.ServingsMin, in.ServingsMax, in.Coating, in.Composition, in.NoticeID, string(allergens), string(linesJSON),
		in.SourceHash())
}

func (s *Store) command(ctx context.Context, name, sql string, args ...any) (Result, error) {
	var r Result
	if err := s.DB.QueryRow(ctx, sql, args...).Scan(&r.ID, &r.Replayed); err != nil {
		return Result{}, gatewayError(name, err)
	}
	return r, nil
}

// invalidFields maps GK422 messages to the input field they concern.
var invalidFields = map[string]string{
	"name_invalid": "name", "body_invalid": "body", "cake_invalid": "name", "state_invalid": "state",
	"flavour_unknown": "flavour_id", "variant_invalid": "size_code", "notice_unknown": "allergen_notice_id",
	"allergens_required": "allergens", "allergens_duplicate": "allergens", "allergens_invalid": "allergens", "lines_invalid": "lines",
	"composition_invalid": "composition", "recipe_invalid": "lines", "idempotency_key_invalid": "Idempotency-Key",
	"expected_version_required": "expected_version", "colour_unknown": "colour_id", "code_invalid": "code",
	"label_invalid": "label", "position_invalid": "position",
}

var invalidMessages = map[string]string{
	"flavour_unknown": "is not an active flavour", "notice_unknown": "is not a recorded allergen notice",
	"allergens_required": "must declare at least one allergen", "allergens_duplicate": "must not repeat an allergen",
	"expected_version_required": "is required",
	"colour_unknown":            "is not an active colour",
}

var conflictCodes = map[string]bool{"stale_version": true, "key_reused": true, "state_conflict": true, "duplicate": true}

// gatewayError maps the gateway's refusal codes; anything else is an
// unavailable dependency or a fault.
func gatewayError(op string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("catalog: %s: %w", op, err)
	}
	switch pgErr.Code {
	case "GK403":
		return ErrForbidden
	case "GK404":
		return ErrNotFound
	case "GK409":
		if conflictCodes[pgErr.Message] {
			return &ConflictError{Code: pgErr.Message}
		}
		return &ConflictError{Code: "state_conflict"}
	case "GK422":
		field, ok := invalidFields[pgErr.Message]
		if !ok {
			field = "input"
		}
		msg, ok := invalidMessages[pgErr.Message]
		if !ok {
			msg = "is not valid"
		}
		return &InvalidError{Field: field, Message: msg}
	}
	return fmt.Errorf("catalog: %s: %w", op, err)
}
