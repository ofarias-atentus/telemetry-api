// Command atdevice-api es la API ingestora central de inventario y telemetría.
//
// Subcomandos:
//
//	atdevice-api serve            - comprueba el esquema y sirve la API HTTP
//	atdevice-api migrate status   - compara la versión aplicada con la esperada
//	atdevice-api migrate up       - aplica las migraciones pendientes
//	atdevice-api migrate baseline - registra el historial en una base creada con los scripts antiguos
//
// serve nunca migra. Códigos de salida: 0 correcto, 1 versión del esquema
// distinta, pendiente o sin historial, 2 uso, configuración, conectividad u
// otro error operativo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"telemetry-api/internal/migrate"
)

const migrateTimeout = 5 * time.Minute

const usage = `uso:
  atdevice-api serve [--host HOST] [--port PUERTO]   (por defecto todas las interfaces y SERVER_PORT)
  atdevice-api migrate status      muestra la versión esperada y la aplicada (salida 1 si no coinciden)
  atdevice-api migrate up          aplica las migraciones pendientes
  atdevice-api migrate baseline    registra el historial en una base creada con los scripts SQL antiguos
  atdevice-api help

La configuración sale de las variables de entorno y del archivo .env del directorio actual.
`

// exitError lleva el código de salida del proceso: 1 versión del esquema
// distinta, 2 uso, configuración, conectividad u otro error operativo.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }

func (e *exitError) Unwrap() error { return e.err }

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error(strings.ReplaceAll(err.Error(), "\n", "; "))
		var xe *exitError
		if errors.As(err, &xe) {
			os.Exit(xe.code)
		}
		os.Exit(exitCodeFor(err))
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return &exitError{code: 2, err: errors.New("falta el subcomando")}
	}
	switch args[0] {
	case "-h", "-help", "--help", "help":
		_, _ = fmt.Fprint(os.Stdout, usage)
		return nil
	case "serve":
		if err := runServe(args[1:]); err != nil {
			return asExitError(err)
		}
		return nil
	case "migrate":
		fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return &exitError{code: 2, err: err}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return &exitError{code: 2, err: errors.New("migrate requiere una acción: status|up|baseline")}
		}
		if len(rest) > 1 {
			return &exitError{code: 2, err: fmt.Errorf("migrate %s no acepta argumentos: %v", rest[0], rest[1:])}
		}
		switch rest[0] {
		case "status":
			return asExitError(migrateStatus(context.Background()))
		case "up":
			return asExitError(migrateUp(context.Background()))
		case "baseline":
			return asExitError(migrateBaseline(context.Background()))
		default:
			return &exitError{code: 2, err: fmt.Errorf("acción de migrate desconocida %q: use status|up|baseline", rest[0])}
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		return &exitError{code: 2, err: fmt.Errorf("subcomando desconocido %q", args[0])}
	}
}

// asExitError conserva un exitError existente y envuelve el resto con el
// código que le corresponde.
func asExitError(err error) error {
	if err == nil {
		return nil
	}
	var xe *exitError
	if errors.As(err, &xe) {
		return err
	}
	return &exitError{code: exitCodeFor(err), err: err}
}

// exitCodeFor manda los desajustes de versión (el operador debe migrar) al
// código 1 y todo lo demás al 2.
func exitCodeFor(err error) int {
	if errors.Is(err, migrate.ErrVersionMismatch) || errors.Is(err, migrate.ErrNotVersioned) {
		return 1
	}
	return 2
}
