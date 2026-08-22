# typed: false
# frozen_string_literal: true

class HarnessMemory < Formula
  desc "Shared Postgres memory for coding agents"
  homepage "https://github.com/Pzharyuk/harness-memory"
  license "MIT"
  head "https://github.com/Pzharyuk/harness-memory.git", branch: "main"

  url "https://github.com/Pzharyuk/harness-memory/archive/d2941c6bc841b91d87d5a9dcf6fc91fd4f958222.tar.gz"
  sha256 "e0416cbf3e4522749dc8faf74efc932c62e8d96938b951e5378664e7a71165d4"
  version "0.1.0"

  depends_on "go" => :build
  depends_on "postgresql@16"

  def install
    ldflags = "-s -w"
    system "go", "build", "-trimpath", "-ldflags", ldflags, "-o", bin/"memoryd", "./cmd/memoryd"
    system "go", "build", "-trimpath", "-ldflags", ldflags, "-o", bin/"memory", "./cmd/memory"
  end

  service do
    run [opt_bin/"memoryd"]
    keep_alive true
    working_dir var
    log_path var/"log/memoryd.log"
    error_log_path var/"log/memoryd.log"
    environment_variables MEMORY_LISTEN: "127.0.0.1:8741",
                          MEMORY_DATABASE_URL: "postgres://localhost/memory?sslmode=disable"
  end

  def caveats
    <<~EOS
      Docs: https://github.com/Pzharyuk/harness-memory/blob/main/docs/install.md

      Local brain (Postgres on this Mac):

        brew services start postgresql@16
        createdb memory
        brew services start harness-memory
        memory init
        memory token create --harness claude

      Cluster brain (do NOT start the local service):

        export MEMORY_URL=https://memory.onit.systems
        memory init
        memory token create --harness grok

      Cluster Access is church office IP only (97.120.177.5).
    EOS
  end

  test do
    assert_path_exists bin/"memoryd"
    assert_path_exists bin/"memory"
    assert_match "usage: memory", shell_output("#{bin}/memory -h")
  end
end
