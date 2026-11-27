# atelemetry-mobile

Servicio para despliegue de la imagen docker de atelemetry-mobile (API de ingesta de
inventario y telemetría de dispositivos) en Ubuntu 20.04.

El paquete `.deb` instala en `/srv/atelemetry-mobile`:

- `docker-compose.yml`: copia de referencia (solo si no existe).
- `.env`: copia de la plantilla con permisos 600 (solo si no existe).

y registra la unidad systemd `atelemetry-mobile.service`. Nunca se sobrescribe un
`.env` ni un `docker-compose.yml` existentes.

## Requisitos previos

El paquete no depende de Docker (solo de `debconf`), por lo que debe instalarse
antes `docker-ce` y el plugin `docker-compose-plugin` (para que exista el
comando `docker compose`), siguiendo la documentación oficial de Docker para
Ubuntu.

### PostgreSQL

La base de datos suele estar en el mismo servidor. El contenedor la alcanza con
`DB_HOST=host.docker.internal` (ya configurado en la plantilla y en
`extra_hosts` del compose), por lo que PostgreSQL debe aceptar conexiones desde
la red de Docker:

1. En `postgresql.conf`, escuchar en una interfaz alcanzable desde el bridge,
   por ejemplo `listen_addresses = 'localhost,172.17.0.1'` (o `'*'`).
2. En `pg_hba.conf`, permitir el rango de Docker:

   ```
   host  <base>  <usuario>  172.17.0.0/16  scram-sha-256
   ```

   Ojo: `docker compose` crea su propia red para el proyecto (por ejemplo
   `172.18.0.0/16`), no usa el bridge por defecto `172.17.0.0/16`. Revisar el
   rango real con `docker network inspect atelemetry-mobile_default` y permitir ese
   rango.
3. Recargar: `sudo systemctl reload postgresql`.
4. Si hay firewall (ufw), permitir el puerto 5432 desde ese rango.

## Instalación

```bash
sudo dpkg -i atelemetry-mobile_1.0.0_all.deb
```

Se pedirá el usuario del sistema que será dueño de `/srv/atelemetry-mobile`
(por defecto `atentus`). En la primera instalación el servicio queda habilitado
pero no iniciado. Pasos siguientes:

1. Editar `/srv/atelemetry-mobile/.env` (`DB_HOST`, `DB_USER`, `DB_PASSWORD`,
   `DB_NAME`). Los valores con `$`, `#` o espacios van entre comillas simples.
2. Revisar el estado de las migraciones:

   ```bash
   docker compose -f /srv/atelemetry-mobile/docker-compose.yml run --rm api migrate status
   ```

3. Respaldar con `pg_dump` y aplicar las migraciones pendientes (pueden
   detener la ingesta mientras corren):

   ```bash
   docker compose -f /srv/atelemetry-mobile/docker-compose.yml run --rm api migrate up
   ```

   Para bases creadas con los scripts psql antiguos (`database.sql` y
   `migrations/`), en vez de `migrate up` usar:

   ```bash
   docker compose -f /srv/atelemetry-mobile/docker-compose.yml run --rm api migrate baseline
   ```

4. Iniciar el servicio:

   ```bash
   sudo systemctl start atelemetry-mobile
   ```

`serve` nunca migra y se niega a iniciar si la base de datos no está en la
última versión de migración.

## Actualización

Al actualizar el paquete, `preinst` detiene el servicio y `postinst` actualiza
la línea `ATELEMETRY_MOBILE_TAG=` de `/srv/atelemetry-mobile/.env` (la agrega si falta) a
la versión de imagen del paquete. Docker Compose lee ese `.env` para interpolar
`docker-compose.yml`, que nunca se sobrescribe. El servicio NO se reinicia
automáticamente. Pasos:

```bash
docker compose -f /srv/atelemetry-mobile/docker-compose.yml pull
docker compose -f /srv/atelemetry-mobile/docker-compose.yml run --rm api migrate status
# si hay migraciones pendientes (respaldar antes con pg_dump):
docker compose -f /srv/atelemetry-mobile/docker-compose.yml run --rm api migrate up
sudo systemctl restart atelemetry-mobile
```

## Si el servicio se detiene con código 1

`serve` termina con código 1 cuando la versión del esquema no coincide (hay
migraciones pendientes). La unidad usa `RestartPreventExitStatus=1`, por lo que
systemd no lo reinicia en bucle, y `docker compose up --abort-on-container-exit
--exit-code-from api` propaga el código de salida del contenedor (un `up`
simple no lo garantiza). Si el servicio queda detenido con código 1, ejecutar
`migrate status` y, si corresponde, `migrate up`. Además hay un límite de
5 arranques por 60 s (`StartLimitBurst`).

El código 1 también puede venir del propio Docker Compose (por ejemplo, fallo
al descargar la imagen o falta del archivo `.env`). Antes de asumir una
discrepancia de esquema, revisar `journalctl -u atelemetry-mobile`.

## Configuración

- Variables en `/srv/atelemetry-mobile/.env`; `LOG_FORMAT=json` y `SERVER_PORT=8080`
  los fija el compose.
- Tag de imagen: variable `ATELEMETRY_MOBILE_TAG` (por defecto `1.0.0`). Puerto
  publicado: `ATELEMETRY_MOBILE_PORT` (por defecto `8080`). Ambas están en
  `/srv/atelemetry-mobile/.env`; el paquete gestiona `ATELEMETRY_MOBILE_TAG`.
- La API no tiene autenticación ni TLS: publicar solo en redes confiables.

## Logs

```bash
journalctl -u atelemetry-mobile -f
docker compose -f /srv/atelemetry-mobile/docker-compose.yml logs -f
```

## Desinstalación

```bash
sudo apt remove atelemetry-mobile
sudo rm /etc/systemd/system/atelemetry-mobile.service
sudo rm -rf /srv/atelemetry-mobile/
```
