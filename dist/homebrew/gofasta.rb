class Gofasta < Formula
  desc "CLI for Gofasta, a Go backend toolkit"
  homepage "https://github.com/gofastadev/cli"
  version "0.1.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/gofastadev/cli/releases/download/v#{version}/gofasta-darwin-arm64"
      sha256 "UPDATE_SHA256_HERE"

      def install
        bin.install "gofasta-darwin-arm64" => "gofasta"
      end
    else
      url "https://github.com/gofastadev/cli/releases/download/v#{version}/gofasta-darwin-amd64"
      sha256 "UPDATE_SHA256_HERE"

      def install
        bin.install "gofasta-darwin-amd64" => "gofasta"
      end
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/gofastadev/cli/releases/download/v#{version}/gofasta-linux-arm64"
      sha256 "UPDATE_SHA256_HERE"

      def install
        bin.install "gofasta-linux-arm64" => "gofasta"
      end
    else
      url "https://github.com/gofastadev/cli/releases/download/v#{version}/gofasta-linux-amd64"
      sha256 "UPDATE_SHA256_HERE"

      def install
        bin.install "gofasta-linux-amd64" => "gofasta"
      end
    end
  end

  test do
    assert_match "Gofasta", shell_output("#{bin}/gofasta --help")
  end
end
