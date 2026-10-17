class Codeaf < Formula
  desc "Terminal coding harness built for open models"
  homepage "https://github.com/Agent-Field/codeaf"
  license "Apache-2.0"

  on_macos do
    on_intel do
      url "https://github.com/Agent-Field/codeaf/releases/download/v0.8.0/codeaf-darwin-amd64"
      sha256 "f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2"
    end
    on_arm do
      url "https://github.com/Agent-Field/codeaf/releases/download/v0.8.0/codeaf-darwin-arm64"
      sha256 "19b55289b5d8bf3345b5e3fae0682cc75ed0a05f5207f772f9f60c6d303589bf"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/Agent-Field/codeaf/releases/download/v0.8.0/codeaf-linux-amd64"
      sha256 "1d99c52d4dbba1d62599d820bbd62342240c4585c3577fca959a16ee2a4d8f72"
    end
    on_arm do
      url "https://github.com/Agent-Field/codeaf/releases/download/v0.8.0/codeaf-linux-arm64"
      sha256 "aee2d1b60f81d5b375b7df4db25fea580e8683880074f6896db6212022f26baa"
    end
  end

  def install
    executables = Dir["codeaf-*"].select { |path| File.file?(path) }
    raise "expected exactly one codeaf release executable" unless executables.length == 1

    bin.install executables.first => "codeaf"
    chmod 0755, bin/"codeaf"
  end

  test do
    assert_path_exists bin/"codeaf"
    assert_match "codeaf ", shell_output("#{bin}/codeaf --version")
  end
end