import pathlib as pl

# get script path
script_path = pl.Path(__file__).resolve()

# get project root (3 levels up)
project_root = script_path.parent.parent.parent

# get changelog path
changelog_path = pl.Path(project_root, "CHANGELOG.md")

# get changelog content
with open(changelog_path, "r") as f:
    changelog = f.read()

    # we need to extract everyting between the first and second header with tags
    changelog = changelog.split("## ")[1].split("## ")[0]

    # remove the first line
    changelog = "\n".join(changelog.split("\n")[1:])

    # trim newlines
    changelog = changelog.strip()

    # write to file
    with open(pl.Path(project_root, "changelog-temp.md"), "w") as ft:
        ft.write(changelog)

