package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/savorum/savorum/internal/library"
	"github.com/savorum/savorum/internal/recipe"
)

func (s *Store) listRecipePage(ctx context.Context, options library.ListOptions, withTotal bool) ([]library.Summary, int, error) {
	if options.State == library.StateActive {
		var err error

		options.Search, err = s.expandNameSearch(ctx, options.Search)
		if err != nil {
			return nil, 0, err
		}
	}

	if options.GroupTranslations && options.State == library.StateActive {
		sets, err := s.translationSets(ctx)
		if err != nil {
			return nil, 0, err
		}

		if len(sets) > 0 {
			return s.listGroupedRecipePage(ctx, options, sets)
		}
	}

	search := options.Search

	fuzzyRanked := options.Sort == library.ListSortRelevance && len(search.TextAlternatives) > 0
	ranked := options.Sort == library.ListSortRelevance && searchExpression(search.Text) != "" && !fuzzyRanked

	if ranked {
		// The ranked page's FTS join already supplies the text predicate.
		search.Text = ""
	}

	where, args, possible := organizationSearchWhereContext(ctx, search, options.State)
	if !possible {
		return []library.Summary{}, 0, nil
	}

	where, args = restrictRecipeIDs(where, args, options.EntryIDs)

	order, err := recipeListOrder(options.Sort)
	if err != nil {
		return nil, 0, err
	}

	page := recipePageSQL{where: where, order: order, pagination: "LIMIT ? OFFSET ?", args: args}

	switch {
	case fuzzyRanked:
		page = fuzzyRankedPage(ctx, page, options, withTotal)
	case ranked:
		page = rankedPage(ctx, page, options, withTotal)
	default:
		page = unrankedPage(ctx, page, options, withTotal)
	}

	rows, err := s.db.QueryContext(ctx, page.prefix+`
		SELECT recipes.id, recipes.document_id, recipes.title, recipes.description,
		       recipes.authors_json, recipes.active_time, recipes.total_time, recipes.revision,
		       recipes.created_at, recipes.updated_at, recipes.deleted_at,
		       primary_usage.asset_id, primary_usage.local_asset_id,
		       COALESCE(primary_blob.media_type, ''), COALESCE(primary_usage.alt, ''),
		       COALESCE(primary_usage.caption, ''), primary_blob.storage_key IS NOT NULL,
		       CASE WHEN recipes.deleted_at IS NULL THEN COALESCE(user_state.favorite, 0) ELSE 0 END,
		       COALESCE(user_state.times_cooked, 0), user_state.last_cooked, recipes.language`+page.totalColumn+`
		FROM library_recipes AS recipes
		LEFT JOIN recipe_revision_assets AS primary_usage
		  ON primary_usage.entry_id = recipes.id
		 AND primary_usage.revision = recipes.revision
		 AND primary_usage.asset_id = recipes.primary_asset_id
		 AND primary_usage.is_primary = 1
		LEFT JOIN assets AS primary_blob ON primary_blob.id = primary_usage.asset_id
		LEFT JOIN recipe_user_state AS user_state
		  ON user_state.entry_id = recipes.id AND user_state.user_id = ?
		`+page.searchJoin+` WHERE `+page.where+`
		ORDER BY `+page.order+`
		`+page.pagination, page.args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query library recipes: %w", err)
	}
	defer rows.Close()

	entries := make([]library.Summary, 0)
	total := 0

	for rows.Next() {
		entry, rowTotal, err := scanSummaryTotal(rows, withTotal)
		if err != nil {
			return nil, 0, fmt.Errorf("scan library recipe: %w", err)
		}

		total = rowTotal

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate library recipes: %w", err)
	}

	if err := rows.Close(); err != nil {
		return nil, 0, fmt.Errorf("close library recipes: %w", err)
	}

	// An empty page at offset zero means no matches; only a page past the end
	// needs the match count a window row cannot carry.
	if withTotal && len(entries) == 0 && options.Offset > 0 {
		total, err = s.countSummaries(ctx, options.Search, options.State)
		if err != nil {
			return nil, 0, err
		}
	}

	if options.State == library.StateActive {
		if err := s.populateRecipeSummaryAssociations(ctx, entries); err != nil {
			return nil, 0, err
		}
	}

	return entries, total, nil
}

// recipePageSQL holds the pieces of the recipe list query that vary with how
// the page is ranked.
type recipePageSQL struct {
	prefix, searchJoin, totalColumn, where, pagination, order string
	args                                                      []any
}

func fuzzyRankedPage(ctx context.Context, q recipePageSQL, options library.ListOptions, withTotal bool) recipePageSQL {
	totalColumn := q.totalColumn
	where, order, args := q.where, q.order, q.args

	var prefix, searchJoin, pagination string

	exactWhere, exactArgs := exactNameTextWhere(options.Search)
	window := ""

	if withTotal {
		window = ", COUNT(*) OVER() AS total"
		totalColumn = ", matching_page.total"
	}

	prefix = `WITH exact_matches AS MATERIALIZED (
		SELECT rowid, bm25(library_recipes_fts, 20, 10, 1, 4) AS score
		FROM library_recipes_fts WHERE library_recipes_fts MATCH ?
	), matching_page AS MATERIALIZED (
		SELECT recipes.rowid, CASE WHEN exact_matches.rowid IS NOT NULL OR (` + exactWhere + `) THEN 0 ELSE 1 END AS quality,
		       COALESCE(exact_matches.score, 0) AS score` + window + `
		FROM library_recipes AS recipes LEFT JOIN exact_matches ON exact_matches.rowid=recipes.rowid
		WHERE ` + where + `
		ORDER BY quality, score, recipes.updated_at DESC, recipes.id LIMIT ? OFFSET ?
	) `

	args = append(append([]any{searchExpression(options.Search.Text)}, exactArgs...), args...)
	args = append(args, options.Limit, options.Offset, currentUserID(ctx))
	searchJoin = `JOIN matching_page ON matching_page.rowid=recipes.rowid`
	order = "matching_page.quality, matching_page.score, " + order
	where, pagination = "1", ""

	return recipePageSQL{
		prefix: prefix, searchJoin: searchJoin, totalColumn: totalColumn,
		where: where, pagination: pagination, order: order, args: args,
	}
}

func rankedPage(ctx context.Context, q recipePageSQL, options library.ListOptions, withTotal bool) recipePageSQL {
	totalColumn := q.totalColumn
	where, order, args := q.where, q.order, q.args

	var prefix, searchJoin, pagination string

	// Rank and filter recipe identities before joining assets and user
	// summary data. Broad matches must not pay those joins for every
	// candidate when only one page is returned. Materialization preserves
	// that boundary; both orders retain the same stable tie breakers.
	prefix = `WITH ranked_page AS MATERIALIZED (
            SELECT recipes.rowid, bm25(library_recipes_fts, 20, 10, 1, 4) AS score
            FROM library_recipes AS recipes
            JOIN library_recipes_fts ON library_recipes_fts.rowid=recipes.rowid
              AND library_recipes_fts MATCH ?
            WHERE ` + where + `
            ORDER BY score, recipes.updated_at DESC, recipes.id
            LIMIT ? OFFSET ?
        ) `

	if withTotal {
		// The window total counts every ranked match before the page's
		// LIMIT applies, so the search WHERE still runs once.
		prefix = `WITH ranked_matches AS MATERIALIZED (
            SELECT recipes.rowid, bm25(library_recipes_fts, 20, 10, 1, 4) AS score,
                   recipes.updated_at, recipes.id
            FROM library_recipes AS recipes
            JOIN library_recipes_fts ON library_recipes_fts.rowid=recipes.rowid
              AND library_recipes_fts MATCH ?
            WHERE ` + where + `
        ), ranked_page AS (
            SELECT rowid, score, COUNT(*) OVER() AS total
            FROM ranked_matches
            ORDER BY score, updated_at DESC, id
            LIMIT ? OFFSET ?
        ) `
		totalColumn = ", ranked_page.total"
	}

	args = append([]any{searchExpression(options.Search.Text)}, args...)
	args = append(args, options.Limit, options.Offset, currentUserID(ctx))
	searchJoin = `JOIN ranked_page ON ranked_page.rowid=recipes.rowid`
	order = "ranked_page.score, " + order
	where, pagination = "1", ""

	return recipePageSQL{
		prefix: prefix, searchJoin: searchJoin, totalColumn: totalColumn,
		where: where, pagination: pagination, order: order, args: args,
	}
}

func unrankedPage(ctx context.Context, q recipePageSQL, options library.ListOptions, withTotal bool) recipePageSQL {
	prefix, searchJoin, totalColumn := q.prefix, q.searchJoin, q.totalColumn
	where, pagination, order, args := q.where, q.pagination, q.order, q.args

	if withTotal {
		// Bound the page at recipe identity before joining assets and
		// user summary data; a window function on the outer SELECT
		// would pay those joins for every match.
		prefix = `WITH matching_page AS MATERIALIZED (
            SELECT recipes.rowid, COUNT(*) OVER() AS total
            FROM library_recipes AS recipes
            LEFT JOIN recipe_user_state AS user_state
              ON user_state.entry_id = recipes.id AND user_state.user_id = ?
            WHERE ` + where + `
            ORDER BY ` + order + `
            LIMIT ? OFFSET ?
        ) `
		totalColumn = ", matching_page.total"

		args = append([]any{currentUserID(ctx)}, args...)
		args = append(args, options.Limit, options.Offset, currentUserID(ctx))
		searchJoin = `JOIN matching_page ON matching_page.rowid=recipes.rowid`
		where, pagination = "1", ""
	} else {
		args = append([]any{currentUserID(ctx)}, args...)
		args = append(args, options.Limit, options.Offset)
	}

	return recipePageSQL{
		prefix: prefix, searchJoin: searchJoin, totalColumn: totalColumn,
		where: where, pagination: pagination, order: order, args: args,
	}
}

func recipeListOrder(value library.ListSort) (string, error) {
	switch value {
	case library.ListSortRelevance, library.ListSortUpdatedDesc:
		return "recipes.updated_at DESC, recipes.id", nil
	case library.ListSortTitleAsc:
		return "recipes.title COLLATE NOCASE ASC, recipes.id", nil
	case library.ListSortRatingDesc:
		return "user_state.rating IS NULL ASC, user_state.rating DESC, recipes.updated_at DESC, recipes.id", nil
	default:
		return "", fmt.Errorf("unsupported recipe list sort %q: %w", value, library.ErrInvalidSearch)
	}
}

func (s *Store) populateRecipeSummaryAssociations(ctx context.Context, entries []library.Summary) error {
	if len(entries) == 0 {
		return nil
	}

	indexes := make(map[library.EntryID]int, len(entries))

	args := make([]any, len(entries))
	for index := range entries {
		indexes[entries[index].ID] = index
		args[index] = entries[index].ID
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(entries)), ",")

	rows, err := s.db.QueryContext(ctx, `
		SELECT recipe_tags.entry_id, tags.id, tags.name, COALESCE(tags.color, '')
		FROM recipe_tags
		JOIN tags ON tags.id = recipe_tags.tag_id
		WHERE recipe_tags.entry_id IN (`+placeholders+`)
		ORDER BY recipe_tags.entry_id, tags.name COLLATE NOCASE, tags.id`, args...)
	if err != nil {
		return fmt.Errorf("query batched recipe tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			entryID library.EntryID
			tag     library.Tag
		)
		if err := rows.Scan(&entryID, &tag.ID, &tag.Name, &tag.Color); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan batched recipe tag: %w", err)
		}

		if index, ok := indexes[entryID]; ok {
			entries[index].Tags = append(entries[index].Tags, tag)
		}
	}

	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate batched recipe tags: %w", err)
	}

	if err := rows.Close(); err != nil {
		return fmt.Errorf("close batched recipe tags: %w", err)
	}

	// Curated membership and cookbook provenance are reported separately.
	if err := s.appendSummaryBooks(ctx, entries, indexes, args, `
		SELECT book_entries.entry_id, books.id, books.kind, books.title
		FROM book_entries
		JOIN books ON books.id = book_entries.book_id
		WHERE book_entries.entry_id IN (`+placeholders+`)
		ORDER BY book_entries.entry_id, books.title COLLATE NOCASE, books.id`,
		func(summary *library.Summary, book library.BookSummary) {
			summary.Books = append(summary.Books, book)
		}); err != nil {
		return fmt.Errorf("query batched recipe book membership: %w", err)
	}

	if err := s.appendSummaryBooks(ctx, entries, indexes, args, `
		SELECT DISTINCT recipe_sources.entry_id, books.id, books.kind, books.title
		FROM recipe_sources
		JOIN books ON books.id = recipe_sources.publication_book_id
		WHERE recipe_sources.entry_id IN (`+placeholders+`)
		ORDER BY recipe_sources.entry_id, books.title COLLATE NOCASE, books.id`,
		func(summary *library.Summary, book library.BookSummary) {
			summary.SourceBooks = append(summary.SourceBooks, book)
		}); err != nil {
		return fmt.Errorf("query batched recipe source books: %w", err)
	}

	return nil
}

func (s *Store) appendSummaryBooks(
	ctx context.Context,
	entries []library.Summary,
	indexes map[library.EntryID]int,
	args []any,
	query string,
	collect func(*library.Summary, library.BookSummary),
) error {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query summary books: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			entryID library.EntryID
			book    library.BookSummary
		)
		if err := rows.Scan(&entryID, &book.ID, &book.Kind, &book.Title); err != nil {
			return fmt.Errorf("scan summary book: %w", err)
		}

		if index, ok := indexes[entryID]; ok {
			collect(&entries[index], book)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate summary books: %w", err)
	}

	return nil
}

// countSummaries is the unauthenticated share of Count; the rare empty
// out-of-range ListPage asks it for the total a window row cannot carry.
func (s *Store) countSummaries(ctx context.Context, search library.SearchCriteria, state library.State) (int, error) {
	if state == library.StateActive {
		var err error

		search, err = s.expandNameSearch(ctx, search)
		if err != nil {
			return 0, err
		}
	}

	where, args, possible := organizationSearchWhereContext(ctx, search, state)
	if !possible {
		return 0, nil
	}

	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM library_recipes AS recipes WHERE `+where, args...)

	var total int
	if err := row.Scan(&total); err != nil {
		return 0, fmt.Errorf("count library recipes: %w", err)
	}

	return total, nil
}

func organizationSearchWhereContext(ctx context.Context, search library.SearchCriteria, state library.State) (string, []any, bool) {
	// A bound OR over both states hides the active/deleted index from the
	// planner. Resolve the validated enum to a fixed predicate so broad
	// searches can count from the covering index without loading documents.
	stateCondition := "recipes.deleted_at IS NULL"

	switch state {
	case library.StateActive:
	case library.StateTrash:
		stateCondition = "recipes.deleted_at IS NOT NULL"
	default:
		return "", nil, false
	}

	conditions := []string{stateCondition, "recipes.library_id = ?"}

	args := []any{scopeID(ctx)}

	textConditions, textArgs, possible := searchTextConditions(search)
	if !possible {
		return "", nil, false
	}

	conditions = append(conditions, textConditions...)
	args = append(args, textArgs...)

	for _, value := range search.Authors {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM recipe_author_entries AS author_entry
			JOIN recipe_author_names AS author_name ON author_name.library_id=author_entry.library_id AND author_name.name=author_entry.name
			WHERE author_entry.entry_id=recipes.id AND author_name.library_id=recipes.library_id AND author_name.normalized_name=?
		)`)
		args = append(args, library.NormalizeAuthorName(value))
	}

	for _, value := range search.Ingredients {
		expression := searchExpression(value)
		if expression == "" {
			return "", nil, false
		}

		conditions = append(conditions, `recipes.id IN (
			SELECT ingredient.entry_id FROM recipe_ingredient_names AS ingredient
			JOIN recipe_ingredient_names_fts ON recipe_ingredient_names_fts.rowid=ingredient.id
			WHERE ingredient.library_id=? AND recipe_ingredient_names_fts MATCH ?
		)`)
		args = append(args, scopeID(ctx), expression)
	}

	// Taxonomy and books are addressed by their stable identifier so a rename
	// never changes what a saved search or a shared filter link means. The
	// normalized display name is still accepted for hand-written queries.
	for _, value := range search.Tags {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM recipe_tags JOIN tags ON tags.id=recipe_tags.tag_id WHERE recipe_tags.entry_id=recipes.id AND (tags.id=? OR tags.normalized_name=?))`)
		args = append(args, value, normalizeName(value))
	}

	facetConditions, facetArgs := searchFacetConditions(search)
	conditions = append(conditions, facetConditions...)
	args = append(args, facetArgs...)

	// Book membership is curated placement; provenance is reached through
	// source:.
	for _, value := range search.Books {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM book_entries
			JOIN books ON books.id=book_entries.book_id
			WHERE book_entries.entry_id=recipes.id AND books.deleted_at IS NULL AND (books.id=? OR lower(books.title)=?)
		)`)
		args = append(args, value, normalizeName(value))
	}

	for _, value := range search.Sources {
		like := "%" + normalizeName(value) + "%"

		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM recipe_sources
			LEFT JOIN books ON books.id=recipe_sources.publication_book_id
			WHERE recipe_sources.entry_id=recipes.id
			  AND (recipe_sources.publication_book_id=?
			       OR lower(recipe_sources.url || ' ' || recipe_sources.original_title || ' ' || recipe_sources.attribution || ' ' || COALESCE(books.title,'')) LIKE ?)
		)`)
		args = append(args, value, like)
	}

	// The recipes one import saved; skipped and failed items have no entry.
	for _, value := range search.Imports {
		conditions = append(conditions, `EXISTS (
			SELECT 1 FROM import_job_items
			JOIN import_jobs ON import_jobs.id=import_job_items.job_id
			WHERE import_job_items.entry_id=recipes.id
			  AND import_job_items.job_id=?
			  AND import_job_items.state='imported'
			  AND import_jobs.library_id=recipes.library_id
		)`)
		args = append(args, value)
	}

	if search.Favorite != nil {
		conditions = append(conditions, `COALESCE((SELECT favorite FROM recipe_user_state WHERE entry_id=recipes.id AND user_id=?),0)=?`)
		args = append(args, currentUserID(ctx), *search.Favorite)
	}

	if search.MinRating != nil {
		conditions = append(conditions, `(SELECT rating FROM recipe_user_state WHERE entry_id=recipes.id AND user_id=?)>=?`)
		args = append(args, currentUserID(ctx), *search.MinRating)
	}
	// Never cooked means neither a recorded date nor a recorded count, so a
	// recipe marked "cooked 7 times" is never offered as one to try.
	if search.MadeNever {
		conditions = append(conditions, `NOT EXISTS (SELECT 1 FROM recipe_user_state WHERE entry_id=recipes.id AND user_id=? AND (last_cooked IS NOT NULL OR times_cooked > 0))`)
		args = append(args, currentUserID(ctx))
	}

	for _, diet := range search.StatedDiets {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM json_each(recipes.document_json, '$.dietary_claims') AS claim
			WHERE json_extract(claim.value, '$.diet') = ? AND json_extract(claim.value, '$.suitability') = 'suitable'
			AND json_extract(claim.value, '$.condition') IS NULL AND json_extract(claim.value, '$.variant_ref') IS NULL)`)
		args = append(args, strings.ReplaceAll(strings.ToLower(strings.TrimSpace(diet)), "-", "_"))
	}

	for _, substance := range search.StatedFreeFrom {
		conditions = append(conditions, `EXISTS (SELECT 1 FROM json_each(recipes.document_json, '$.allergen_declarations') AS claim
			WHERE lower(json_extract(claim.value, '$.substance')) = lower(?) AND json_extract(claim.value, '$.presence') = 'free_from'
			AND json_extract(claim.value, '$.condition') IS NULL AND json_extract(claim.value, '$.variant_ref') IS NULL)`)
		args = append(args, strings.TrimSpace(substance))
	}

	if search.MissingContent != "" {
		ingredients := `COALESCE(json_array_length(recipes.document_json, '$.ingredient_sections'), 0)=0`
		instructions := `COALESCE(json_array_length(recipes.document_json, '$.instruction_sections'), 0)=0`

		switch search.MissingContent {
		case "ingredients":
			conditions = append(conditions, ingredients)
		case "instructions":
			conditions = append(conditions, instructions)
		case "either":
			conditions = append(conditions, "("+ingredients+" OR "+instructions+")")
		case "both":
			conditions = append(conditions, "("+ingredients+" AND "+instructions+")")
		default:
			return "", nil, false
		}
	}

	if search.ReviewRequired != nil {
		condition := `EXISTS (
			SELECT 1
			FROM json_each(recipes.document_json, '$.ingredient_sections') AS ingredient_section
			JOIN json_each(ingredient_section.value, '$.ingredients') AS ingredient
			WHERE json_array_length(json_extract(ingredient.value, '$.normalization.warnings')) > 0
			  AND COALESCE(json_extract(ingredient.value, '$.normalization.review'), '') <> 'confirmed'
			  AND NOT (
				json_extract(ingredient.value, '$.normalization.method') = 'inferred'
				AND json_extract(ingredient.value, '$.normalization.review') = 'corrected'
				AND json_array_length(json_extract(ingredient.value, '$.normalization.warnings')) = 1
				AND (
				      json_extract(ingredient.value, '$.normalization.warnings[0]') LIKE ? || '%'
				   OR json_extract(ingredient.value, '$.normalization.warnings[0]') = ?
				)
			  )
		)`

		// The prefix marks an interpretation somebody chose during review; the
		// sentence after it says what the source supplied, so it varies. The
		// second comparison recognizes rows written before that sentence had to
		// be true, which must not fall back into the review queue.
		args = append(args, recipe.ImportReviewUnitPrefix, recipe.ImportReviewUnitWarningLegacy)

		if !*search.ReviewRequired {
			condition = "NOT " + condition
		}

		conditions = append(conditions, condition)
	}

	return strings.Join(conditions, " AND "), args, true
}

// searchTextConditions builds the full-text predicates for the search text and
// its typo alternatives. It reports false when the text can match nothing.
func searchTextConditions(search library.SearchCriteria) ([]string, []any, bool) {
	var (
		conditions []string
		args       []any
	)

	if search.Text != "" {
		expression := searchExpression(search.Text)
		if expression == "" {
			return nil, nil, false
		}

		if len(search.TextAlternatives) == 0 {
			conditions = append(conditions, `recipes.rowid IN (
                SELECT rowid FROM library_recipes_fts WHERE library_recipes_fts MATCH ?
            )`)
			args = append(args, expression)
		} else {
			for _, word := range library.SearchWords(search.Text) {
				condition := `recipes.rowid IN (SELECT rowid FROM library_recipes_fts WHERE library_recipes_fts MATCH ?)`

				args = append(args, searchExpression(word))
				if alternatives := search.TextAlternatives[word]; len(alternatives) > 0 {
					values := alternatives
					placeholders := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
					condition = "(" + condition + ` OR EXISTS (
						SELECT 1 FROM recipe_name_term_entries AS term_entry
						WHERE term_entry.entry_id=recipes.id AND term_entry.library_id=recipes.library_id
						AND term_entry.term IN (` + placeholders + ")) )"

					for _, value := range values {
						args = append(args, value)
					}
				}

				conditions = append(conditions, condition)
			}
		}
	}

	return conditions, args, true
}

// searchFacetConditions builds the course, cuisine, method, and dietary
// predicates.
func searchFacetConditions(search library.SearchCriteria) ([]string, []any) {
	var (
		conditions []string
		args       []any
	)

	for _, kind := range []library.FacetKind{library.FacetCourse, library.FacetCuisine, library.FacetMethod, library.FacetDietary} {
		for _, value := range search.Facets[kind] {
			portableMember := map[library.FacetKind]string{
				library.FacetCourse:  "courses",
				library.FacetCuisine: "cuisines",
				library.FacetMethod:  "cooking_methods",
			}[kind]
			if portableMember != "" {
				// Course, cuisine, and method may be either authored recipe content
				// or optional local organization. Query both without creating or
				// assigning a facet for the portable value. A stable facet identifier
				// resolves through the local catalog; a hand-written qualifier still
				// works by normalized display name.
				conditions = append(conditions, fmt.Sprintf(`(EXISTS (
					SELECT 1 FROM recipe_facets JOIN facets ON facets.id=recipe_facets.facet_id
					WHERE recipe_facets.entry_id=recipes.id AND facets.kind=?
					  AND (facets.id=? OR facets.normalized_name=?))
				OR EXISTS (
					SELECT 1 FROM json_each(recipes.document_json, '$.%s') AS portable_value
					WHERE lower(trim(CAST(portable_value.value AS TEXT))) = COALESCE(
						(SELECT normalized_name FROM facets WHERE kind=? AND id=?), ?
					)
				))`, portableMember))
				args = append(args, kind, value, normalizeName(value), kind, value, normalizeName(value))

				continue
			}

			conditions = append(conditions, `EXISTS (SELECT 1 FROM recipe_facets JOIN facets ON facets.id=recipe_facets.facet_id WHERE recipe_facets.entry_id=recipes.id AND facets.kind=? AND (facets.id=? OR facets.normalized_name=?))`)
			args = append(args, kind, value, normalizeName(value))
		}
	}

	return conditions, args
}

func searchExpression(query string) string {
	terms := library.SearchWords(query)
	for index, term := range terms {
		terms[index] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"*`
	}

	return strings.Join(terms, " AND ")
}

func (s *Store) translationSets(ctx context.Context) ([][]library.TranslationMember, error) {
	var present bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM library_recipes
		WHERE library_id = ? AND deleted_at IS NULL AND translations_json <> '[]')`, scopeID(ctx)).Scan(&present); err != nil {
		return nil, fmt.Errorf("check for translations: %w", err)
	}

	if !present {
		return nil, nil
	}

	members, err := s.TranslationMembers(ctx)
	if err != nil {
		return nil, err
	}

	return library.TranslationSets(members), nil
}

// listGroupedRecipePage orders every match, replaces each translation set by
// the rendition preferred among its matching members at the position of its
// best-ranked match, and then pages the result.
func (s *Store) listGroupedRecipePage(ctx context.Context, options library.ListOptions, sets [][]library.TranslationMember) ([]library.Summary, int, error) {
	ordered, err := s.matchingRecipeIDs(ctx, options)
	if err != nil {
		return nil, 0, err
	}

	setOf := map[library.EntryID]int{}

	for index, set := range sets {
		for _, member := range set {
			setOf[member.ID] = index
		}
	}

	matched := map[library.EntryID]bool{}
	matchedMembers := make([][]library.TranslationMember, len(sets))

	for _, id := range ordered {
		matched[id] = true

		if index, grouped := setOf[id]; grouped {
			for _, member := range sets[index] {
				if member.ID == id {
					matchedMembers[index] = append(matchedMembers[index], member)
				}
			}
		}
	}

	results := make([]library.EntryID, 0, len(ordered))
	emitted := map[int]bool{}

	for _, id := range ordered {
		index, grouped := setOf[id]
		if !grouped {
			results = append(results, id)
			continue
		}

		if !emitted[index] {
			emitted[index] = true
			results = append(results, library.PreferredRendition(matchedMembers[index], options.PreferredLanguages))
		}
	}

	// Without a search every recipe matches, which says nothing about why.
	searching := strings.TrimSpace(options.Query) != ""
	total := len(results)
	start := min(options.Offset, total)
	end := min(start+options.Limit, total)

	entries, err := s.summariesByID(ctx, results[start:end])
	if err != nil {
		return nil, 0, err
	}

	for position := range entries {
		index, grouped := setOf[entries[position].ID]
		if !grouped {
			continue
		}

		for _, member := range sets[index] {
			if member.ID != entries[position].ID {
				entries[position].Translations = append(entries[position].Translations, library.SummaryRendition{
					ID: member.ID, Title: member.Title, Language: member.Language, Matched: searching && matched[member.ID],
				})
			}
		}
	}

	if err := s.populateRecipeSummaryAssociations(ctx, entries); err != nil {
		return nil, 0, err
	}

	return entries, total, nil
}

// matchingRecipeIDs returns the identity of every match in list order.
func (s *Store) matchingRecipeIDs(ctx context.Context, options library.ListOptions) ([]library.EntryID, error) {
	search := options.Search
	fuzzyRanked := options.Sort == library.ListSortRelevance && len(search.TextAlternatives) > 0
	ranked := options.Sort == library.ListSortRelevance && searchExpression(search.Text) != "" && !fuzzyRanked

	if ranked {
		search.Text = ""
	}

	where, args, possible := organizationSearchWhereContext(ctx, search, options.State)
	if !possible {
		return nil, nil
	}

	order, err := recipeListOrder(options.Sort)
	if err != nil {
		return nil, err
	}

	query := `SELECT recipes.id FROM library_recipes AS recipes
		LEFT JOIN recipe_user_state AS user_state
		  ON user_state.entry_id = recipes.id AND user_state.user_id = ?
		WHERE ` + where + ` ORDER BY ` + order

	args = append([]any{currentUserID(ctx)}, args...)

	if fuzzyRanked {
		exactWhere, exactArgs := exactNameTextWhere(options.Search)
		query = `WITH exact_matches AS MATERIALIZED (
			SELECT rowid, bm25(library_recipes_fts, 20, 10, 1, 4) AS score
			FROM library_recipes_fts WHERE library_recipes_fts MATCH ?
		) SELECT recipes.id FROM library_recipes AS recipes
			LEFT JOIN exact_matches ON exact_matches.rowid=recipes.rowid
			WHERE ` + where + `
			ORDER BY CASE WHEN exact_matches.rowid IS NOT NULL OR (` + exactWhere + `) THEN 0 ELSE 1 END,
			COALESCE(exact_matches.score, 0), recipes.updated_at DESC, recipes.id`
		args[0] = searchExpression(options.Search.Text)
		args = append(args, exactArgs...)
	} else if ranked {
		query = `SELECT recipes.id FROM library_recipes AS recipes
			JOIN library_recipes_fts ON library_recipes_fts.rowid=recipes.rowid
			  AND library_recipes_fts MATCH ?
			WHERE ` + where + `
			ORDER BY bm25(library_recipes_fts, 20, 10, 1, 4), recipes.updated_at DESC, recipes.id`
		args[0] = searchExpression(options.Search.Text)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query matching recipes: %w", err)
	}
	defer rows.Close()

	ids := []library.EntryID{}

	for rows.Next() {
		var id library.EntryID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan matching recipe: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate matching recipes: %w", err)
	}

	return ids, nil
}

// summariesByID reads summaries for the given entries, in the given order.
func (s *Store) summariesByID(ctx context.Context, ids []library.EntryID) ([]library.Summary, error) {
	if len(ids) == 0 {
		return []library.Summary{}, nil
	}

	args := make([]any, 0, len(ids)+1)
	args = append(args, currentUserID(ctx))

	for _, id := range ids {
		args = append(args, id)
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")

	rows, err := s.db.QueryContext(ctx, `
		SELECT recipes.id, recipes.document_id, recipes.title, recipes.description,
		       recipes.authors_json, recipes.active_time, recipes.total_time, recipes.revision,
		       recipes.created_at, recipes.updated_at, recipes.deleted_at,
		       primary_usage.asset_id, primary_usage.local_asset_id,
		       COALESCE(primary_blob.media_type, ''), COALESCE(primary_usage.alt, ''),
		       COALESCE(primary_usage.caption, ''), primary_blob.storage_key IS NOT NULL,
		       CASE WHEN recipes.deleted_at IS NULL THEN COALESCE(user_state.favorite, 0) ELSE 0 END,
		       COALESCE(user_state.times_cooked, 0), user_state.last_cooked, recipes.language
		FROM library_recipes AS recipes
		LEFT JOIN recipe_revision_assets AS primary_usage
		  ON primary_usage.entry_id = recipes.id
		 AND primary_usage.revision = recipes.revision
		 AND primary_usage.asset_id = recipes.primary_asset_id
		 AND primary_usage.is_primary = 1
		LEFT JOIN assets AS primary_blob ON primary_blob.id = primary_usage.asset_id
		LEFT JOIN recipe_user_state AS user_state
		  ON user_state.entry_id = recipes.id AND user_state.user_id = ?
		WHERE recipes.id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query library recipes: %w", err)
	}
	defer rows.Close()

	byID := make(map[library.EntryID]library.Summary, len(ids))

	for rows.Next() {
		entry, _, err := scanSummaryTotal(rows, false)
		if err != nil {
			return nil, fmt.Errorf("scan library recipe: %w", err)
		}

		byID[entry.ID] = entry
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate library recipes: %w", err)
	}

	entries := make([]library.Summary, 0, len(ids))
	for _, id := range ids {
		if entry, ok := byID[id]; ok {
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

func restrictRecipeIDs(where string, args []any, ids []library.EntryID) (string, []any) {
	if len(ids) == 0 {
		return where, args
	}

	where += " AND recipes.id IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")"
	for _, id := range ids {
		args = append(args, string(id))
	}

	return where, args
}
