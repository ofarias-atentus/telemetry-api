"""
@package installer
===============
@file   build_package.py
@class  BuildPackage
@author Orlando Farías <ofarias@atentus.com>
        (c) Servicios de Monitoreo S.A. - Santiago, Chile
@brief  Contiene clase para generar instalables para Mac o Ubuntu.
@date   Octubre/2020
@todo
"""

import argparse
from subprocess import call
from os import mkdir
from pathlib import Path
from shutil import rmtree, copytree, copy, ignore_patterns
from sys import version_info


class BuildPackage(object):
    """Clase que se encarga de generar package de instalación."""

    def __init__(self):
        """Constructor."""
        self._parser = argparse.ArgumentParser(
            description="Línea de comandos para generar deb o pkg aplicación atdevice-api."
        )
        self._add_arguments()
        self._rootProject = Path(__file__).resolve().parent.parent
        self._rootInstaller = self._rootProject / "temp"
        self._projectFolder = self._rootInstaller / "atdevice-api"
        self._ignore = ignore_patterns(
            "*.pyc", "temp", "build-installer.py", "__pycache__", "db.sqlite3"
        )

    def _add_arguments(self):
        """Agrega parámetros que se pueden configurar con línea de comandos."""
        self._parser.add_argument(
            "--pkg", action="store_true", help="Construir .pkg para MacOS"
        )
        self._parser.add_argument(
            "--deb", action="store_true", help="Construir .deb para Ubuntu"
        )

    def _handle(self, args):
        """Maneja la ejecución de construcción de package para sistema operativo respectivo.

        Parameters
        ----------
        args
            Argumentos de línea de comandos ingresados
        """
        try:
            if args.pkg:
                print("Construyendo pkg...")
                self._buildPkg(args)
            elif args.deb:
                print("Construyendo deb...")
                self._buildDeb(args)
            else:
                self._parser.print_help()
        except Exception as e:
            import traceback

            print(e)
            print(traceback.format_exc())
        finally:
            from os.path import exists

            if exists(self._rootInstaller):
                rmtree(self._rootInstaller)

    def _buildPkg(self, args):
        """Construye .pkg para mac.

        Parameters
        ----------
        args
            Argumentos de línea de comandos ingresados
        """
        print("No se soporta plataforma mac")
        exit(0)

    def _buildDeb(self, args):
        """Construye .deb para ubuntu.

        Parameters
        ----------
        args
            Argumentos de línea de comandos ingresados
        """
        sources_folder = self._rootProject / "installer" / "ubuntu" / "sources"
        self._addFiles(args)
        copy(
            self._rootProject
            / "installer"
            / "ubuntu"
            / "service"
            / "atdevice-api.service",
            self._projectFolder,
        )
        debian_folder = self._projectFolder / "debian"
        copytree(sources_folder, debian_folder)
        call(
            "cd {} && dpkg-buildpackage -us -uc".format(self._projectFolder), shell=True
        )
        deb = sorted(self._rootInstaller.glob("*.deb"))
        if deb:
            copy(deb[0], self._rootProject)
            print(
                ".deb se ha copiado en el siguiente path: {}".format(self._rootProject)
            )

    def _addFiles(self, args):
        """Agrega archivos necesarios para construir package desde proyecto telemetry-api.

        Parameters
        ----------
        args
            Argumentos de línea de comandos ingresados
        """
        mkdir(self._rootInstaller)
        mkdir(self._projectFolder)
        copy(
            self._rootProject / "installer" / "ubuntu" / "README.md",
            self._projectFolder,
        )
        copy(self._rootProject / "deploy" / "docker-compose.yml", self._projectFolder)
        copy(
            self._rootProject / "deploy" / ".env.docker.example",
            self._projectFolder,
        )

    def start(self):
        """
        Inicia programa de línea de comandos.
        @retval
        """
        args = self._parser.parse_args()
        self._handle(args)


if __name__ == "__main__":
    if version_info[0] < 3:
        print("Solamente se puede construir instalable con python3 en adelante.")
        exit(0)
    buildPackage = BuildPackage()
    buildPackage.start()
