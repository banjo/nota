# Template for Casks/nota.rb in banjo/homebrew-tap.
# scripts/bump-cask.sh fills in @VERSION@ and @SHA256@; don't edit the tap's copy by hand.
cask "nota" do
  version "@VERSION@"
  sha256 "@SHA256@"

  url "https://github.com/banjo/nota/releases/download/v#{version}/Nota-#{version}-arm64.zip"
  name "Nota"
  desc "Markdown notes as plain files, with a CLI and MCP server for agents"
  homepage "https://nota.banjoanton.com"

  depends_on macos: :monterey
  depends_on arch: :arm64

  app "Nota.app"
  # The release build puts the `nota` CLI inside the app bundle.
  binary "#{appdir}/Nota.app/Contents/Resources/bin/nota"

  # Signed ad hoc, not notarized, so Gatekeeper would block it.
  postflight_steps do
    run "/usr/bin/xattr",
        args:           ["-dr", "com.apple.quarantine", "{{appdir}}/Nota.app"],
        writable_paths: ["{{appdir}}/Nota.app"]
  end

  uninstall quit: "com.banjoanton.nota"
end
