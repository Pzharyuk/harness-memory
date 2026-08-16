# typed: false
# frozen_string_literal: true

# Template formula. Fill url and sha256 from the first GitHub Release
# tarball (GoReleaser: harness-memory_<version>_<os>_<arch>.tar.gz).
class HarnessMemory < Formula
  desc "Shared Postgres memory for coding agents"
  homepage "https://github.com/Pzharyuk/harness-memory"
  url "https://github.com/Pzharyuk/harness-memory/releases/download/vX.Y.Z/harness-memory_X.Y.Z_darwin_arm64.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"

  depends_on "postgresql@16"

  def install
    bin.install "memoryd"
    bin.install "memory"
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
      memoryd listens on 127.0.0.1:8741 and reads MEMORY_DATABASE_URL /
      MEMORY_LISTEN from the environment (not config.toml).

      After starting Postgres and creating the database:

        brew services start postgresql@16
        createdb memory
        brew services start harness-memory
        memory init
        memory token create --harness claude
    EOS
  end

  test do
    assert_path_exists bin/"memoryd"
    assert_path_exists bin/"memory"
  end
end
