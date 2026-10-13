CREATE VIEW library_search_content AS
SELECT r.rowid AS rowid, r.id AS entry_id, r.title,
 COALESCE((SELECT group_concat(CAST(value AS TEXT), ' ')
   FROM json_tree(r.document_json, '$.ingredient_sections')
   WHERE key IN ('name','source_text','note','title') AND type='text'), '') AS ingredients,
 r.search_text,
 COALESCE((SELECT group_concat(text, ' ') FROM (
   SELECT tags.name AS text FROM recipe_tags JOIN tags ON tags.id=recipe_tags.tag_id WHERE recipe_tags.entry_id=r.id
   UNION ALL SELECT facets.name FROM recipe_facets JOIN facets ON facets.id=recipe_facets.facet_id WHERE recipe_facets.entry_id=r.id
   UNION ALL SELECT books.title FROM book_entries JOIN books ON books.id=book_entries.book_id WHERE book_entries.entry_id=r.id AND books.deleted_at IS NULL
   UNION ALL SELECT recipe_sources.url || ' ' || recipe_sources.original_title || ' ' || recipe_sources.attribution || ' ' || COALESCE(books.title,'')
     FROM recipe_sources LEFT JOIN books ON books.id=recipe_sources.publication_book_id WHERE recipe_sources.entry_id=r.id
  )), '') AS organization
FROM library_recipes r;

CREATE VIRTUAL TABLE library_recipes_fts USING fts5(
 title, ingredients, search_text, organization,
 tokenize='unicode61 remove_diacritics 2', prefix='2 3 4'
);

CREATE TRIGGER search_recipe_tags_insert AFTER INSERT ON recipe_tags BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.entry_id;
END;

CREATE TRIGGER search_recipe_tags_update AFTER UPDATE ON recipe_tags BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id);
END;

CREATE TRIGGER search_recipe_tags_delete AFTER DELETE ON recipe_tags BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=old.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=old.entry_id;
END;

CREATE TRIGGER search_recipe_facets_insert AFTER INSERT ON recipe_facets BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.entry_id;
END;

CREATE TRIGGER search_recipe_facets_update AFTER UPDATE ON recipe_facets BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id);
END;

CREATE TRIGGER search_recipe_facets_delete AFTER DELETE ON recipe_facets BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=old.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=old.entry_id;
END;

CREATE TRIGGER search_book_entries_insert AFTER INSERT ON book_entries BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.entry_id;
END;

CREATE TRIGGER search_book_entries_update AFTER UPDATE ON book_entries BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id);
END;

CREATE TRIGGER search_book_entries_delete AFTER DELETE ON book_entries BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=old.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=old.entry_id;
END;

CREATE TRIGGER search_recipe_sources_insert AFTER INSERT ON recipe_sources BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.entry_id;
END;

CREATE TRIGGER search_recipe_sources_update AFTER UPDATE ON recipe_sources BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (old.entry_id,new.entry_id);
END;

CREATE TRIGGER search_recipe_sources_delete AFTER DELETE ON recipe_sources BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=old.entry_id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=old.entry_id;
END;

CREATE TRIGGER search_books_rename AFTER UPDATE OF title,deleted_at ON books WHEN old.title<>new.title OR old.deleted_at IS NOT new.deleted_at BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM book_entries WHERE book_id=new.id UNION SELECT entry_id FROM recipe_sources WHERE publication_book_id=new.id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM book_entries WHERE book_id=new.id UNION SELECT entry_id FROM recipe_sources WHERE publication_book_id=new.id);
END;

CREATE TRIGGER library_recipes_fts_insert AFTER INSERT ON library_recipes BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.id;
END;

CREATE TRIGGER library_recipes_fts_delete AFTER DELETE ON library_recipes BEGIN
 DELETE FROM library_recipes_fts WHERE rowid=old.rowid;
END;

CREATE TRIGGER search_tags_rename AFTER UPDATE OF name ON tags WHEN old.name<>new.name BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM recipe_tags WHERE tag_id=new.id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM recipe_tags WHERE tag_id=new.id);
END;

CREATE TRIGGER search_facets_rename AFTER UPDATE OF name ON facets WHEN old.name<>new.name BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM recipe_facets WHERE facet_id=new.id));
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id IN (SELECT entry_id FROM recipe_facets WHERE facet_id=new.id);
END;

CREATE TRIGGER library_recipes_fts_update
AFTER UPDATE OF title, document_json, search_text ON library_recipes BEGIN
 DELETE FROM library_recipes_fts WHERE rowid IN (SELECT rowid FROM library_search_content WHERE entry_id=new.id);
 INSERT INTO library_recipes_fts(rowid,title,ingredients,search_text,organization)
  SELECT rowid,title,ingredients,search_text,organization FROM library_search_content WHERE entry_id=new.id;
END;
