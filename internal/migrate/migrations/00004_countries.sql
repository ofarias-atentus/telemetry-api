-- +goose Up
-- Crea la tabla countries (códigos ISO 3166-1 alfa-2) y convierte devices.country a CHAR(2) con FK a ella.
-- Falla si algún devices.country actual no se puede mapear a un código (por código, nombre o alias);
-- el mensaje lista cada valor con su número de filas y no se modifica ninguna fila de devices.
-- Irreversible: no tiene sección Down. Hacer antes un respaldo con pg_dump -t devices.
-- Se aplica con `atelemetry-mobile migrate up`, con la API detenida: el binario
-- antiguo no resuelve el país y el esquema nuevo rechaza nombres largos.
-- ALTER TYPE reescribe devices con bloqueo exclusivo: la ingesta de telemetría se detiene hasta el COMMIT.
-- La comparación usa lower() de la BD, que en bases con locale C solo pliega ASCII:
-- un valor antiguo con tildes y todo en mayúsculas se listaría como sin mapear.
-- Un test verifica la carga de countries (249 códigos ISO) y que el predicado de país
-- coincida con el de la consulta de upsert de la API.
--
-- Antes de cambiar nada comprueba, y aborta con un mensaje que explica qué hacer si:
--   * devices no existe, ya existe countries o devices_country_fkey, o no somos dueños de devices;
--   * hay vistas, vistas materializadas o reglas que usan devices.country (bloquean ALTER TYPE).
-- Tras crear countries (solo una tabla nueva, que se revierte si algo falla) y antes de tocar
-- devices, lista los valores sin código ISO. Antes de ALTER TYPE verifica que devices conserva
-- sus filas y que todas tienen un código presente en countries.

SET LOCAL lock_timeout = '5s';

-- +goose StatementBegin
DO $$
DECLARE
    deps text;
BEGIN
    IF to_regclass('devices') IS NULL THEN
        RAISE EXCEPTION 'La tabla devices no existe: la base no tiene el esquema original. Revisa con `migrate status` o usa `migrate baseline`.';
    END IF;

    IF NOT pg_has_role(current_user, (SELECT relowner FROM pg_class WHERE oid = 'devices'::regclass), 'USAGE') THEN
        RAISE EXCEPTION 'El rol % no es dueño de devices y no puede alterarla. Ejecuta la migración con el rol dueño de la tabla.', current_user;
    END IF;

    IF to_regclass('countries') IS NOT NULL THEN
        RAISE EXCEPTION 'Ya existe una relación llamada countries: la base está a medio migrar o fue modificada a mano. Revisa el esquema antes de continuar.';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'devices_country_fkey' AND conrelid = 'devices'::regclass) THEN
        RAISE EXCEPTION 'devices ya tiene la restricción devices_country_fkey: la base está a medio migrar o fue modificada a mano.';
    END IF;

    SELECT string_agg(format('%I (%s)', c.relname, r.rulename), ', ') INTO deps
    FROM pg_depend d
    JOIN pg_rewrite r ON d.classid = 'pg_rewrite'::regclass AND d.objid = r.oid
    JOIN pg_class c ON c.oid = r.ev_class
    WHERE d.refclassid = 'pg_class'::regclass
        AND d.refobjid = 'devices'::regclass
        AND d.refobjsubid IN (
            SELECT a.attnum FROM pg_attribute a
            WHERE a.attrelid = 'devices'::regclass AND a.attname = 'country'
        );
    IF deps IS NOT NULL THEN
        RAISE EXCEPTION 'Hay vistas, vistas materializadas o reglas que usan devices.country, que esta migración cambia a CHAR(2): %. Elimínalas (o recréalas después de migrar) y vuelve a ejecutar.', deps;
    END IF;
END $$;
-- +goose StatementEnd

LOCK TABLE devices IN SHARE ROW EXCLUSIVE MODE;

SELECT set_config('migration.devices_rows', count(*)::text, true) FROM devices;

CREATE TABLE countries (
    code                 CHAR(2) PRIMARY KEY,      -- ISO 3166-1 alfa-2, en mayúsculas
    name                 VARCHAR(60) NOT NULL UNIQUE, -- Nombre en español
    aliases              TEXT[] NOT NULL DEFAULT '{}' -- Otras grafías en minúsculas (inglés, sin tildes)
);

-- Si migra un superusuario, countries queda a nombre del dueño de devices, como si migrara el dueño.
-- +goose StatementBegin
DO $$
DECLARE
    dev_owner name;
BEGIN
    SELECT pg_get_userbyid(relowner) INTO dev_owner FROM pg_class WHERE oid = 'devices'::regclass;
    IF dev_owner <> current_user THEN
        EXECUTE format('ALTER TABLE %I OWNER TO %I', 'countries', dev_owner);
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO countries (code, name, aliases) VALUES
    ('AD', 'Andorra', ARRAY[]::text[]),
    ('AE', 'Emiratos Árabes Unidos', ARRAY['united arab emirates', 'emiratos arabes unidos', 'emiratos árabes unidos', 'uae']::text[]),
    ('AF', 'Afganistán', ARRAY['afghanistan', 'afganistan']::text[]),
    ('AG', 'Antigua y Barbuda', ARRAY['antigua and barbuda']::text[]),
    ('AI', 'Anguila', ARRAY['anguilla']::text[]),
    ('AL', 'Albania', ARRAY[]::text[]),
    ('AM', 'Armenia', ARRAY[]::text[]),
    ('AO', 'Angola', ARRAY[]::text[]),
    ('AQ', 'Antártida', ARRAY['antarctica', 'antartida']::text[]),
    ('AR', 'Argentina', ARRAY[]::text[]),
    ('AS', 'Samoa Americana', ARRAY['american samoa']::text[]),
    ('AT', 'Austria', ARRAY[]::text[]),
    ('AU', 'Australia', ARRAY[]::text[]),
    ('AW', 'Aruba', ARRAY[]::text[]),
    ('AX', 'Islas Åland', ARRAY['aland islands', 'åland islands', 'islas aland', 'islas åland']::text[]),
    ('AZ', 'Azerbaiyán', ARRAY['azerbaijan', 'azerbaiyan']::text[]),
    ('BA', 'Bosnia y Herzegovina', ARRAY['bosnia and herzegovina']::text[]),
    ('BB', 'Barbados', ARRAY[]::text[]),
    ('BD', 'Bangladés', ARRAY['bangladesh', 'banglades']::text[]),
    ('BE', 'Bélgica', ARRAY['belgium', 'belgica']::text[]),
    ('BF', 'Burkina Faso', ARRAY[]::text[]),
    ('BG', 'Bulgaria', ARRAY[]::text[]),
    ('BH', 'Baréin', ARRAY['bahrain', 'barein']::text[]),
    ('BI', 'Burundi', ARRAY[]::text[]),
    ('BJ', 'Benín', ARRAY['benin']::text[]),
    ('BL', 'San Bartolomé', ARRAY['saint barthelemy', 'san bartolome']::text[]),
    ('BM', 'Bermudas', ARRAY['bermuda']::text[]),
    ('BN', 'Brunéi', ARRAY['brunei', 'brunei darussalam']::text[]),
    ('BO', 'Bolivia', ARRAY[]::text[]),
    ('BQ', 'Bonaire, San Eustaquio y Saba', ARRAY['bonaire sint eustatius and saba']::text[]),
    ('BR', 'Brasil', ARRAY['brazil']::text[]),
    ('BS', 'Bahamas', ARRAY['the bahamas']::text[]),
    ('BT', 'Bután', ARRAY['bhutan', 'butan']::text[]),
    ('BV', 'Isla Bouvet', ARRAY['bouvet island']::text[]),
    ('BW', 'Botsuana', ARRAY['botswana']::text[]),
    ('BY', 'Bielorrusia', ARRAY['belarus']::text[]),
    ('BZ', 'Belice', ARRAY['belize']::text[]),
    ('CA', 'Canadá', ARRAY['canada']::text[]),
    ('CC', 'Islas Cocos', ARRAY['cocos islands', 'cocos (keeling) islands']::text[]),
    ('CD', 'República Democrática del Congo', ARRAY['democratic republic of the congo', 'democratic republic of congo', 'dr congo', 'rd congo', 'drc', 'rdc', 'republica democratica del congo', 'congo-kinshasa']::text[]),
    ('CF', 'República Centroafricana', ARRAY['central african republic', 'republica centroafricana']::text[]),
    ('CG', 'República del Congo', ARRAY['republic of the congo', 'republica del congo', 'congo-brazzaville']::text[]),
    ('CH', 'Suiza', ARRAY['switzerland']::text[]),
    ('CI', 'Costa de Marfil', ARRAY['ivory coast', 'cote d''ivoire', 'côte d''ivoire', 'cote divoire']::text[]),
    ('CK', 'Islas Cook', ARRAY['cook islands']::text[]),
    ('CL', 'Chile', ARRAY[]::text[]),
    ('CM', 'Camerún', ARRAY['cameroon', 'camerun']::text[]),
    ('CN', 'China', ARRAY[]::text[]),
    ('CO', 'Colombia', ARRAY[]::text[]),
    ('CR', 'Costa Rica', ARRAY[]::text[]),
    ('CU', 'Cuba', ARRAY[]::text[]),
    ('CV', 'Cabo Verde', ARRAY['cape verde']::text[]),
    ('CW', 'Curazao', ARRAY['curacao']::text[]),
    ('CX', 'Isla de Navidad', ARRAY['christmas island']::text[]),
    ('CY', 'Chipre', ARRAY['cyprus']::text[]),
    ('CZ', 'Chequia', ARRAY['czechia', 'czech republic', 'republica checa', 'república checa']::text[]),
    ('DE', 'Alemania', ARRAY['germany']::text[]),
    ('DJ', 'Yibuti', ARRAY['djibouti']::text[]),
    ('DK', 'Dinamarca', ARRAY['denmark']::text[]),
    ('DM', 'Dominica', ARRAY[]::text[]),
    ('DO', 'República Dominicana', ARRAY['dominican republic', 'republica dominicana']::text[]),
    ('DZ', 'Argelia', ARRAY['algeria']::text[]),
    ('EC', 'Ecuador', ARRAY[]::text[]),
    ('EE', 'Estonia', ARRAY[]::text[]),
    ('EG', 'Egipto', ARRAY['egypt']::text[]),
    ('EH', 'Sahara Occidental', ARRAY['western sahara']::text[]),
    ('ER', 'Eritrea', ARRAY[]::text[]),
    ('ES', 'España', ARRAY['spain', 'espana']::text[]),
    ('ET', 'Etiopía', ARRAY['ethiopia', 'etiopia']::text[]),
    ('FI', 'Finlandia', ARRAY['finland']::text[]),
    ('FJ', 'Fiyi', ARRAY['fiji']::text[]),
    ('FK', 'Islas Malvinas', ARRAY['falkland islands', 'islas falkland']::text[]),
    ('FM', 'Micronesia', ARRAY['federated states of micronesia']::text[]),
    ('FO', 'Islas Feroe', ARRAY['faroe islands', 'islas faroe']::text[]),
    ('FR', 'Francia', ARRAY['france']::text[]),
    ('GA', 'Gabón', ARRAY['gabon']::text[]),
    ('GB', 'Reino Unido', ARRAY['united kingdom', 'uk', 'great britain', 'gran bretana', 'gran bretaña']::text[]),
    ('GD', 'Granada', ARRAY['grenada']::text[]),
    ('GE', 'Georgia', ARRAY[]::text[]),
    ('GF', 'Guayana Francesa', ARRAY['french guiana']::text[]),
    ('GG', 'Guernsey', ARRAY[]::text[]),
    ('GH', 'Ghana', ARRAY[]::text[]),
    ('GI', 'Gibraltar', ARRAY[]::text[]),
    ('GL', 'Groenlandia', ARRAY['greenland']::text[]),
    ('GM', 'Gambia', ARRAY['the gambia']::text[]),
    ('GN', 'Guinea', ARRAY[]::text[]),
    ('GP', 'Guadalupe', ARRAY['guadeloupe']::text[]),
    ('GQ', 'Guinea Ecuatorial', ARRAY['equatorial guinea']::text[]),
    ('GR', 'Grecia', ARRAY['greece']::text[]),
    ('GS', 'Islas Georgias del Sur y Sandwich del Sur', ARRAY['south georgia and the south sandwich islands']::text[]),
    ('GT', 'Guatemala', ARRAY[]::text[]),
    ('GU', 'Guam', ARRAY[]::text[]),
    ('GW', 'Guinea-Bisáu', ARRAY['guinea-bissau', 'guinea-bisau', 'guinea bisau', 'guinea bissau']::text[]),
    ('GY', 'Guyana', ARRAY[]::text[]),
    ('HK', 'Hong Kong', ARRAY[]::text[]),
    ('HM', 'Islas Heard y McDonald', ARRAY['heard island and mcdonald islands']::text[]),
    ('HN', 'Honduras', ARRAY[]::text[]),
    ('HR', 'Croacia', ARRAY['croatia']::text[]),
    ('HT', 'Haití', ARRAY['haiti']::text[]),
    ('HU', 'Hungría', ARRAY['hungary', 'hungria']::text[]),
    ('ID', 'Indonesia', ARRAY[]::text[]),
    ('IE', 'Irlanda', ARRAY['ireland']::text[]),
    ('IL', 'Israel', ARRAY[]::text[]),
    ('IM', 'Isla de Man', ARRAY['isle of man']::text[]),
    ('IN', 'India', ARRAY[]::text[]),
    ('IO', 'Territorio Británico del Océano Índico', ARRAY['british indian ocean territory', 'territorio britanico del oceano indico', 'territorio británico del océano índico']::text[]),
    ('IQ', 'Irak', ARRAY['iraq']::text[]),
    ('IR', 'Irán', ARRAY['iran']::text[]),
    ('IS', 'Islandia', ARRAY['iceland']::text[]),
    ('IT', 'Italia', ARRAY['italy']::text[]),
    ('JE', 'Jersey', ARRAY[]::text[]),
    ('JM', 'Jamaica', ARRAY[]::text[]),
    ('JO', 'Jordania', ARRAY['jordan']::text[]),
    ('JP', 'Japón', ARRAY['japan', 'japon']::text[]),
    ('KE', 'Kenia', ARRAY['kenya']::text[]),
    ('KG', 'Kirguistán', ARRAY['kyrgyzstan', 'kirguistan']::text[]),
    ('KH', 'Camboya', ARRAY['cambodia']::text[]),
    ('KI', 'Kiribati', ARRAY[]::text[]),
    ('KM', 'Comoras', ARRAY['comoros']::text[]),
    ('KN', 'San Cristóbal y Nieves', ARRAY['saint kitts and nevis', 'san cristobal y nieves']::text[]),
    ('KP', 'Corea del Norte', ARRAY['north korea', 'korea, north', 'democratic people''s republic of korea']::text[]),
    ('KR', 'Corea del Sur', ARRAY['south korea', 'republic of korea', 'korea, south', 'republica de corea', 'república de corea']::text[]),
    ('KW', 'Kuwait', ARRAY[]::text[]),
    ('KY', 'Islas Caimán', ARRAY['cayman islands', 'islas caiman']::text[]),
    ('KZ', 'Kazajistán', ARRAY['kazakhstan', 'kazajistan']::text[]),
    ('LA', 'Laos', ARRAY[]::text[]),
    ('LB', 'Líbano', ARRAY['lebanon', 'libano']::text[]),
    ('LC', 'Santa Lucía', ARRAY['saint lucia', 'santa lucia']::text[]),
    ('LI', 'Liechtenstein', ARRAY[]::text[]),
    ('LK', 'Sri Lanka', ARRAY[]::text[]),
    ('LR', 'Liberia', ARRAY[]::text[]),
    ('LS', 'Lesoto', ARRAY['lesotho']::text[]),
    ('LT', 'Lituania', ARRAY['lithuania']::text[]),
    ('LU', 'Luxemburgo', ARRAY['luxembourg']::text[]),
    ('LV', 'Letonia', ARRAY['latvia']::text[]),
    ('LY', 'Libia', ARRAY['libya']::text[]),
    ('MA', 'Marruecos', ARRAY['morocco']::text[]),
    ('MC', 'Mónaco', ARRAY['monaco']::text[]),
    ('MD', 'Moldavia', ARRAY['moldova']::text[]),
    ('ME', 'Montenegro', ARRAY[]::text[]),
    ('MF', 'San Martín (Francia)', ARRAY['saint martin', 'saint martin (french part)', 'san martin (francia)']::text[]),
    ('MG', 'Madagascar', ARRAY[]::text[]),
    ('MH', 'Islas Marshall', ARRAY['marshall islands']::text[]),
    ('MK', 'Macedonia del Norte', ARRAY['north macedonia', 'macedonia']::text[]),
    ('ML', 'Malí', ARRAY['mali']::text[]),
    ('MM', 'Myanmar', ARRAY['burma', 'birmania']::text[]),
    ('MN', 'Mongolia', ARRAY[]::text[]),
    ('MO', 'Macao', ARRAY['macau']::text[]),
    ('MP', 'Islas Marianas del Norte', ARRAY['northern mariana islands']::text[]),
    ('MQ', 'Martinica', ARRAY['martinique']::text[]),
    ('MR', 'Mauritania', ARRAY[]::text[]),
    ('MS', 'Montserrat', ARRAY[]::text[]),
    ('MT', 'Malta', ARRAY[]::text[]),
    ('MU', 'Mauricio', ARRAY['mauritius']::text[]),
    ('MV', 'Maldivas', ARRAY['maldives']::text[]),
    ('MW', 'Malaui', ARRAY['malawi']::text[]),
    ('MX', 'México', ARRAY['mexico']::text[]),
    ('MY', 'Malasia', ARRAY['malaysia']::text[]),
    ('MZ', 'Mozambique', ARRAY[]::text[]),
    ('NA', 'Namibia', ARRAY[]::text[]),
    ('NC', 'Nueva Caledonia', ARRAY['new caledonia']::text[]),
    ('NE', 'Níger', ARRAY['niger']::text[]),
    ('NF', 'Isla Norfolk', ARRAY['norfolk island']::text[]),
    ('NG', 'Nigeria', ARRAY[]::text[]),
    ('NI', 'Nicaragua', ARRAY[]::text[]),
    ('NL', 'Países Bajos', ARRAY['netherlands', 'the netherlands', 'paises bajos', 'holanda', 'holland']::text[]),
    ('NO', 'Noruega', ARRAY['norway']::text[]),
    ('NP', 'Nepal', ARRAY[]::text[]),
    ('NR', 'Nauru', ARRAY[]::text[]),
    ('NU', 'Niue', ARRAY[]::text[]),
    ('NZ', 'Nueva Zelanda', ARRAY['new zealand']::text[]),
    ('OM', 'Omán', ARRAY['oman']::text[]),
    ('PA', 'Panamá', ARRAY['panama']::text[]),
    ('PE', 'Perú', ARRAY['peru']::text[]),
    ('PF', 'Polinesia Francesa', ARRAY['french polynesia']::text[]),
    ('PG', 'Papúa Nueva Guinea', ARRAY['papua new guinea', 'papua nueva guinea']::text[]),
    ('PH', 'Filipinas', ARRAY['philippines']::text[]),
    ('PK', 'Pakistán', ARRAY['pakistan']::text[]),
    ('PL', 'Polonia', ARRAY['poland']::text[]),
    ('PM', 'San Pedro y Miquelón', ARRAY['saint pierre and miquelon', 'san pedro y miquelon']::text[]),
    ('PN', 'Islas Pitcairn', ARRAY['pitcairn islands']::text[]),
    ('PR', 'Puerto Rico', ARRAY[]::text[]),
    ('PS', 'Palestina', ARRAY['palestine']::text[]),
    ('PT', 'Portugal', ARRAY[]::text[]),
    ('PW', 'Palaos', ARRAY['palau']::text[]),
    ('PY', 'Paraguay', ARRAY[]::text[]),
    ('QA', 'Catar', ARRAY['qatar']::text[]),
    ('RE', 'Reunión', ARRAY['reunion']::text[]),
    ('RO', 'Rumania', ARRAY['romania']::text[]),
    ('RS', 'Serbia', ARRAY[]::text[]),
    ('RU', 'Rusia', ARRAY['russia', 'russian federation']::text[]),
    ('RW', 'Ruanda', ARRAY['rwanda']::text[]),
    ('SA', 'Arabia Saudita', ARRAY['saudi arabia']::text[]),
    ('SB', 'Islas Salomón', ARRAY['solomon islands', 'islas salomon']::text[]),
    ('SC', 'Seychelles', ARRAY[]::text[]),
    ('SD', 'Sudán', ARRAY['sudan']::text[]),
    ('SE', 'Suecia', ARRAY['sweden']::text[]),
    ('SG', 'Singapur', ARRAY['singapore']::text[]),
    ('SH', 'Santa Elena, Ascensión y Tristán de Acuña', ARRAY['saint helena ascension and tristan da cunha', 'santa elena ascension y tristan de acuna']::text[]),
    ('SI', 'Eslovenia', ARRAY['slovenia']::text[]),
    ('SJ', 'Svalbard y Jan Mayen', ARRAY['svalbard and jan mayen']::text[]),
    ('SK', 'Eslovaquia', ARRAY['slovakia']::text[]),
    ('SL', 'Sierra Leona', ARRAY['sierra leone']::text[]),
    ('SM', 'San Marino', ARRAY[]::text[]),
    ('SN', 'Senegal', ARRAY[]::text[]),
    ('SO', 'Somalia', ARRAY[]::text[]),
    ('SR', 'Surinam', ARRAY['suriname']::text[]),
    ('SS', 'Sudán del Sur', ARRAY['south sudan', 'sudan del sur']::text[]),
    ('ST', 'Santo Tomé y Príncipe', ARRAY['sao tome and principe', 'santo tome y principe']::text[]),
    ('SV', 'El Salvador', ARRAY[]::text[]),
    ('SX', 'San Martín (Países Bajos)', ARRAY['sint maarten', 'sint maarten (dutch part)', 'san martin (paises bajos)']::text[]),
    ('SY', 'Siria', ARRAY['syria']::text[]),
    ('SZ', 'Esuatini', ARRAY['eswatini', 'swaziland', 'suazilandia']::text[]),
    ('TC', 'Islas Turcas y Caicos', ARRAY['turks and caicos islands']::text[]),
    ('TD', 'Chad', ARRAY[]::text[]),
    ('TF', 'Territorios Australes Franceses', ARRAY['french southern territories']::text[]),
    ('TG', 'Togo', ARRAY[]::text[]),
    ('TH', 'Tailandia', ARRAY['thailand']::text[]),
    ('TJ', 'Tayikistán', ARRAY['tajikistan', 'tayikistan']::text[]),
    ('TK', 'Tokelau', ARRAY[]::text[]),
    ('TL', 'Timor Oriental', ARRAY['timor-leste', 'timor leste', 'east timor']::text[]),
    ('TM', 'Turkmenistán', ARRAY['turkmenistan']::text[]),
    ('TN', 'Túnez', ARRAY['tunisia', 'tunez']::text[]),
    ('TO', 'Tonga', ARRAY[]::text[]),
    ('TR', 'Turquía', ARRAY['turkey', 'turkiye', 'turquia']::text[]),
    ('TT', 'Trinidad y Tobago', ARRAY['trinidad and tobago']::text[]),
    ('TV', 'Tuvalu', ARRAY[]::text[]),
    ('TW', 'Taiwán', ARRAY['taiwan']::text[]),
    ('TZ', 'Tanzania', ARRAY[]::text[]),
    ('UA', 'Ucrania', ARRAY['ukraine']::text[]),
    ('UG', 'Uganda', ARRAY[]::text[]),
    ('UM', 'Islas ultramarinas de Estados Unidos', ARRAY['united states minor outlying islands']::text[]),
    ('US', 'Estados Unidos', ARRAY['united states', 'united states of america', 'usa', 'eeuu', 'ee.uu.', 'ee. uu.', 'u.s.', 'u.s.a.', 'estados unidos de america', 'estados unidos de américa']::text[]),
    ('UY', 'Uruguay', ARRAY[]::text[]),
    ('UZ', 'Uzbekistán', ARRAY['uzbekistan']::text[]),
    ('VA', 'Ciudad del Vaticano', ARRAY['vatican city', 'holy see', 'vaticano', 'santa sede']::text[]),
    ('VC', 'San Vicente y las Granadinas', ARRAY['saint vincent and the grenadines']::text[]),
    ('VE', 'Venezuela', ARRAY[]::text[]),
    ('VG', 'Islas Vírgenes Británicas', ARRAY['british virgin islands', 'islas virgenes britanicas']::text[]),
    ('VI', 'Islas Vírgenes de los Estados Unidos', ARRAY['us virgin islands', 'united states virgin islands', 'islas virgenes de los estados unidos']::text[]),
    ('VN', 'Vietnam', ARRAY['viet nam']::text[]),
    ('VU', 'Vanuatu', ARRAY[]::text[]),
    ('WF', 'Wallis y Futuna', ARRAY['wallis and futuna']::text[]),
    ('WS', 'Samoa', ARRAY[]::text[]),
    ('YE', 'Yemen', ARRAY[]::text[]),
    ('YT', 'Mayotte', ARRAY[]::text[]),
    ('ZA', 'Sudáfrica', ARRAY['south africa', 'sudafrica']::text[]),
    ('ZM', 'Zambia', ARRAY[]::text[]),
    ('ZW', 'Zimbabue', ARRAY['zimbabwe']::text[])
ON CONFLICT (code) DO NOTHING;

-- +goose StatementBegin
DO $$
DECLARE
    unmapped TEXT;
BEGIN
    SELECT string_agg(format('"%s" (%s filas)', u.country, u.n), ', ' ORDER BY u.country) INTO unmapped
    FROM (
        SELECT d.country, count(*) AS n
        FROM devices d
        WHERE NOT EXISTS (
            SELECT 1 FROM countries c
            WHERE c.code = upper(btrim(d.country))
                OR lower(c.name) = lower(btrim(d.country))
                OR lower(btrim(d.country)) = ANY(c.aliases)
        )
        GROUP BY d.country
    ) AS u;
    IF unmapped IS NOT NULL THEN
        RAISE EXCEPTION 'Valores de country sin código ISO: %. Corrige devices.country a un nombre o código válido (UPDATE devices SET country = ... WHERE country = ...) y vuelve a ejecutar; no se modificó nada.', unmapped;
    END IF;
END $$;
-- +goose StatementEnd

UPDATE devices d SET country = (
    SELECT c.code FROM countries c
    WHERE c.code = upper(btrim(d.country))
        OR lower(c.name) = lower(btrim(d.country))
        OR lower(btrim(d.country)) = ANY(c.aliases)
);

-- +goose StatementBegin
DO $$
DECLARE
    rows_before bigint := current_setting('migration.devices_rows')::bigint;
    total bigint;
    without_code bigint;
BEGIN
    SELECT count(*) INTO total FROM devices;
    SELECT count(*) INTO without_code
    FROM devices d
    WHERE d.country IS NULL OR NOT EXISTS (SELECT 1 FROM countries c WHERE c.code = d.country);

    IF total <> rows_before THEN
        RAISE EXCEPTION 'devices cambió de % a % filas durante la migración; se revierte. Detén la ingesta y reintenta.', rows_before, total;
    END IF;
    IF without_code > 0 THEN
        RAISE EXCEPTION '% dispositivos quedaron sin un código de país válido tras el UPDATE; se revierte.', without_code;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE devices ALTER COLUMN country TYPE CHAR(2);

ALTER TABLE devices ADD CONSTRAINT devices_country_fkey FOREIGN KEY (country) REFERENCES countries(code);
