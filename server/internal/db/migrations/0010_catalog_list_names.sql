-- Lists downloaded from the two regional domain lists of the catalog take
-- the catalog's names for them, in the language the list was named in. A
-- name already taken by another list stays where it is.
UPDATE OR IGNORE route_lists SET name =
    CASE WHEN name GLOB '*[А-Яа-яЁё]*' THEN 'Россия: для пользователей внутри страны' ELSE 'Russia: for users inside the country' END
 WHERE url = 'https://raw.githubusercontent.com/itdoginfo/allow-domains/main/Russia/inside-raw.lst';
UPDATE OR IGNORE route_lists SET name =
    CASE WHEN name GLOB '*[А-Яа-яЁё]*' THEN 'Россия: для пользователей за рубежом' ELSE 'Russia: for users abroad' END
 WHERE url = 'https://raw.githubusercontent.com/itdoginfo/allow-domains/main/Russia/outside-raw.lst';
