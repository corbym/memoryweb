# typed: false
# frozen_string_literal: true

# NOTE: This source formula's version and SHA256 hashes are maintained
# automatically by `.github/workflows/update-homebrew.yml` on each release.
# If the hashes are empty, run that workflow (or `brew audit`) to refresh
# them before publishing — stale hashes cause `brew install` failures.

class Memoryweb < Formula
  desc "Persistent knowledge graph MCP server for AI agents"
  homepage "https://github.com/corbym/memoryweb"
  version "1.54.10"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_darwin_amd64.tar.gz"
      sha256 ""
    end

    on_arm do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_darwin_arm64.tar.gz"
      sha256 ""
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_linux_amd64.tar.gz"
      sha256 ""
    end

    on_arm do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_linux_arm64.tar.gz"
      sha256 ""
    end
  end

  def install
    arch = Hardware::CPU.intel? ? "amd64" : "arm64"
    os   = OS.mac? ? "darwin" : "linux"
    dir  = "memoryweb_#{os}_#{arch}"

    bin.install "#{dir}/memoryweb"
    (share/"memoryweb/hooks").install Dir["#{dir}/hooks/*"]
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/memoryweb --version 2>&1")
  end
end
