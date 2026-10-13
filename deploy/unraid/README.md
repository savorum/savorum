# Savorum for Unraid

Savorum is a free, open-source recipe manager for keeping and using your own
cookbook. Import recipes from websites and other apps, organize them into books,
plan meals, shop for ingredients, and cook with the batch size you need.
Your library and photographs stay on your server.

[Explore Savorum](https://savorum.app/) ·
[Documentation](https://docs.savorum.app/) ·
[Source](https://github.com/savorum/savorum) ·
[Get help](https://savorum.app/contact/)

## Install

1. **Prepare your library folder.** Open the Unraid terminal and create a
   dedicated, empty appdata directory. For the default path:

   ```sh
   mkdir -p /mnt/user/appdata/savorum
   chown 65532:65532 /mnt/user/appdata/savorum
   chmod 0700 /mnt/user/appdata/savorum
   ```

   Savorum runs as user and group `65532:65532`, so it needs to own this
   folder. If you choose another path, use it in all three commands and in the
   template. For existing data, back it up and follow
   [Docker](https://docs.savorum.app/admin/docker/).

2. **Install Savorum from Apps** once the template is published. Set these fields:

   | Field | What to enter |
   | --- | --- |
   | Web interface port | A free port on Unraid; the default is `8080`. |
   | Recipe library | The folder you prepared; the default is `/mnt/user/appdata/savorum`. |
   | Savorum address | The address you will visit, such as `http://192.168.1.10:8080`. Use your server's actual IP and chosen port, with no path or trailing slash. |

   Leave **Network discovery** at `false` in Advanced View. The template uses
   bridge networking; connect clients using your Savorum address.

3. **Start Savorum and open WebUI.** Import a recipe link or add a recipe of
   your own. Your recipes, photographs, books, settings, and other library data
   are kept in the Recipe library folder across container restarts and updates.
   Enable Unraid autostart if you want Savorum to start with the server.

## Accounts and remote access

The default local library has no sign-in: everyone who can reach the app uses
that library. Keep it on a trusted private network. For a shared installation,
follow [Accounts](https://docs.savorum.app/admin/accounts/).

For access outside your home, follow
[Remote access](https://docs.savorum.app/admin/remote-access/).
Set **Savorum address** to the HTTPS address of your proxy, and configure
`SAVORUM_TRUSTED_PROXIES` for its immediate address. Open that HTTPS address;
the template's WebUI shortcut opens the direct mapped HTTP port.

Other settings are described in
[Configure](https://docs.savorum.app/admin/configure/). The image does not
support `PUID` or `PGID` variables.

## Backups and setup help

- **Back up your library:** follow
  [Backup and restore](https://docs.savorum.app/admin/backup-restore/)
  and keep backups on separate storage. A plain backup excludes webhook keys;
  preserve them separately or use an encrypted backup. Stop Savorum before
  making an appdata filesystem copy, and before restoring.
- **Permission denied in the logs:** confirm the Recipe library folder belongs
  to `65532:65532` with mode `0700`. Do not run the container as root or make
  the folder world-writable.
- **WebUI refuses the address:** make Savorum address match the IP or hostname
  and host port you are visiting, then apply the template changes. Changing
  the web port also requires changing Savorum address.
- **Check readiness:** use `docker exec Savorum /savorum healthcheck` in the
  Unraid terminal. If you renamed the container, use that name. The image has
  no shell, so its Console menu cannot provide a shell session.
- **Importing PDFs and photographs:** the template uses the document-tools
  image, which reads English text. Set a memory limit in Extra Parameters,
  such as `--memory=2g`, if other people can upload files.
- **Updates:** the template follows the latest release. Back up before
  applying an update from the Docker tab. To stay on one version, set the
  repository to a full version such as `ghcr.io/savorum/savorum:v0.1.0-documents`.

## License

[LICENSE](LICENSE) is an unchanged copy of Savorum's GNU Affero General Public
License v3.0. Savorum's name and brand rights remain with their owners.
