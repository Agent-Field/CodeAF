
cask "codeaf" do
  version "0.8.0"

  sha256 arm:   "19b55289b5d8bf3345b5e3fae0682cc75ed0a05f5207f772f9f60c6d303589bf",
         intel: "f926117df3f9d369bfdaa1960060f1d17bec8df6b3fdc1f25f9b6a9fba18cef2"

  url "https://github.com/Agent-Field/codeaf/releases/download/v#{version}/codeaf-darwin-#{Hardware::CPU.arm? ? "arm64" : "amd64"}"
  name "codeaf"
  desc "Terminal coding harness built for open models"
  homepage "https://github.com/Agent-Field/codeaf"

  binary "codeaf-darwin-#{Hardware::CPU.arm? ? "arm64" : "amd64"}", target: "codeaf"

  postflight do
    set_permissions 0755, staged_path/"codeaf-darwin-*"
  end

  livecheck do
    url :url
    strategy :github_latest
  end

  test do
    assert_match "codeaf ", shell_output("#{bin}/codeaf --version")
  end
end
