# typed: false
# frozen_string_literal: true

class Memoryweb < Formula
  desc "Persistent knowledge graph MCP server for AI agents"
  homepage "https://github.com/corbym/memoryweb"
  version "1.4.3"
  license "MIT"

  on_macos do
    on_intel do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_darwin_amd64.tar.gz"
      sha256 "f9c042313880bb49082c49f155f6348af69284df45d2ef22252b8158a97cf824"
    end

    on_arm do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_darwin_arm64.tar.gz"
      sha256 "c6c67c21677b0310924fee32f843636f0ccdfc6124936b31af381f154f5771a5"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_linux_amd64.tar.gz"
      sha256 "4e51a05cb227795b0c7a7b6e89faab49a542ec95ebfa2d0dcf44c0c0cce6e5bf"
    end

    on_arm do
      url "https://github.com/corbym/memoryweb/releases/download/v#{version}/memoryweb_v#{version}_linux_arm64.tar.gz"
      sha256 "f8c6d010d94b509359b01e133d1599b0b79d34f7f2f3363fa898ec393236b414"
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
