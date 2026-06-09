import java.nio.file.Path;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

public final class LwyLinkNetExample {
    private static final Path DEFAULT_EXE = Path.of("").toAbsolutePath().normalize()
            .resolve("build")
            .resolve("lwylink-net.exe");

    public static void main(String[] args) throws Exception {
        int code = run(args);
        if (code != 0) {
            System.exit(code);
        }
    }

    private static int run(String[] args) throws Exception {
        Cli cli = Cli.parse(args);
        if (cli.selfTest) {
            return selfTest(cli.exe);
        }
        if ("connect".equals(cli.command)) {
            return runConnect(cli);
        }
        if ("cleanup".equals(cli.command)) {
            return runCleanup(cli);
        }
        printUsage();
        return 2;
    }

    private static int selfTest(Path exe) throws Exception {
        try (LwyLink.NetEngine engine = new LwyLink.NetEngine(exe)) {
            Process help = new ProcessBuilder(engine.exe().toString(), "--help")
                    .redirectErrorStream(true)
                    .start();
            int helpCode = help.waitFor();
            if (helpCode != 0) {
                System.err.println("unexpected help return code from lwylink-net.exe: " + helpCode);
                return helpCode;
            }

            LwyLink.ConnectOptions options = new LwyLink.ConnectOptions();
            options.token = "self-test-token";
            options.userID = 1;
            options.roomID = 1;
            options.roomPublicKey = "self-test-room-key";
            options.networkCIDR = "172.16.1.0/24";
            options.overlayAddr = "127.0.0.1:1";
            options.serverPublicKey = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";

            List<Map<String, String>> events = new ArrayList<>();
            engine.start(options, event -> {
                synchronized (events) {
                    events.add(event);
                }
            });

            long deadline = System.nanoTime() + java.time.Duration.ofSeconds(8).toNanos();
            while (System.nanoTime() < deadline) {
                synchronized (events) {
                    if (hasEventTypes(events, "preparing", "connecting")) {
                        System.out.println("self-test ok: Java launched lwylink-net.exe and parsed JSON status events");
                        return 0;
                    }
                }
                Thread.sleep(50);
            }

            synchronized (events) {
                System.err.println("self-test did not receive expected JSON events");
                System.err.println(events);
            }
            return 1;
        }
    }

    private static int runConnect(Cli cli) throws Exception {
        LwyLink.ConnectOptions options = new LwyLink.ConnectOptions();
        options.api = cli.value("api", LwyLink.env("LWYLINK_API", LwyLink.DEFAULT_API));
        options.token = cli.value("token", LwyLink.env("LWYLINK_TOKEN", ""));
        options.roomID = longValue(cli.value("room-id", LwyLink.env("LWYLINK_ROOM_ID", "0")));
        options.ifName = cli.value("if-name", LwyLink.env("LWYLINK_IF", "LwyLink"));
        options.userID = longValue(cli.value("user-id", LwyLink.env("LWYLINK_USER_ID", "0")));
        options.roomName = cli.value("room-name", LwyLink.env("LWYLINK_ROOM_NAME", ""));
        options.roomPublicKey = cli.value("room-public-key", LwyLink.env("LWYLINK_ROOM_PUBLIC_KEY", ""));
        options.networkCIDR = cli.value("network-cidr", LwyLink.env("LWYLINK_NETWORK_CIDR", ""));
        options.overlayAddr = cli.value("overlay-addr", LwyLink.env("LWYLINK_OVERLAY_ADDR", ""));
        options.udpOverlayAddr = cli.value("udp-overlay-addr", LwyLink.env("LWYLINK_UDP_OVERLAY_ADDR", ""));
        options.stun = cli.value("stun", LwyLink.env("LWYLINK_STUN", ""));
        options.serverPublicKey = cli.value("server-public-key", LwyLink.env("LWYLINK_SERVER_PUBLIC_KEY", ""));

        try (LwyLink.NetEngine engine = new LwyLink.NetEngine(cli.exe)) {
            Runtime.getRuntime().addShutdownHook(new Thread(engine::close));
            engine.start(options, LwyLinkNetExample::handleEvent);
            return engine.waitFor();
        }
    }

    private static int runCleanup(Cli cli) throws Exception {
        try (LwyLink.NetEngine engine = new LwyLink.NetEngine(cli.exe)) {
            return engine.cleanup(
                    cli.value("if-name", LwyLink.env("LWYLINK_IF", "LwyLink")),
                    cli.value("cidr", LwyLink.env("LWYLINK_NETWORK_CIDR", ""))
            );
        }
    }

    private static void handleEvent(Map<String, String> event) {
        String type = event.getOrDefault("type", "unknown");
        switch (type) {
            case "connected" -> System.out.printf(
                    "connected ip=%s gateway=%s mtu=%s%n",
                    event.getOrDefault("ip", "-"),
                    event.getOrDefault("gateway", "-"),
                    event.getOrDefault("mtu", "-")
            );
            case "transport" -> System.out.printf(
                    "transport mode=%s message=%s%n",
                    event.getOrDefault("transportMode", "-"),
                    event.getOrDefault("transportMessage", "")
            );
            case "error" -> System.err.printf("error: %s%n", event.getOrDefault("message", ""));
            default -> {
                String message = event.getOrDefault("message", "");
                System.out.println(message.isBlank() ? type : type + ": " + message);
            }
        }
    }

    private static long longValue(String value) {
        if (value == null || value.isBlank()) {
            return 0;
        }
        try {
            return Long.parseLong(value);
        } catch (NumberFormatException ignored) {
            return 0;
        }
    }

    private static boolean hasEventTypes(List<Map<String, String>> events, String... expected) {
        Set<String> types = new HashSet<>();
        for (Map<String, String> event : events) {
            types.add(event.get("type"));
        }
        for (String type : expected) {
            if (!types.contains(type)) {
                return false;
            }
        }
        return true;
    }

    private static void printUsage() {
        System.out.println("""
                Usage:
                  javac examples/java/LwyLink.java examples/java/LwyLinkNetExample.java
                  java -cp examples/java LwyLinkNetExample --self-test
                  java -cp examples/java LwyLinkNetExample connect [options]
                  java -cp examples/java LwyLinkNetExample cleanup [options]

                Common options:
                  --exe <path>              Path to build/lwylink-net.exe
                  --token <token>           Login token; prefer LWYLINK_TOKEN
                  --api <url>               Control API URL
                  --room-id <id>            Room ID
                  --if-name <name>          Wintun adapter name
                  --stun <host:port>        STUN server for UDP candidate discovery
                """);
    }

    private static final class Cli {
        final String command;
        final boolean selfTest;
        final Path exe;
        final Map<String, String> options;

        Cli(String command, boolean selfTest, Path exe, Map<String, String> options) {
            this.command = command;
            this.selfTest = selfTest;
            this.exe = exe;
            this.options = options;
        }

        static Cli parse(String[] args) {
            String command = "";
            boolean selfTest = false;
            Path exe = DEFAULT_EXE;
            Map<String, String> options = new HashMap<>();
            for (int i = 0; i < args.length; i++) {
                String arg = args[i];
                if ("--self-test".equals(arg)) {
                    selfTest = true;
                    continue;
                }
                if ("--exe".equals(arg) && i + 1 < args.length) {
                    exe = Path.of(args[++i]);
                    continue;
                }
                if (!arg.startsWith("--") && command.isBlank()) {
                    command = arg;
                    continue;
                }
                if (arg.startsWith("--")) {
                    String key = arg.substring(2);
                    String value = "true";
                    int equals = key.indexOf('=');
                    if (equals >= 0) {
                        value = key.substring(equals + 1);
                        key = key.substring(0, equals);
                    } else if (i + 1 < args.length && !args[i + 1].startsWith("--")) {
                        value = args[++i];
                    }
                    options.put(key, value);
                }
            }
            return new Cli(command, selfTest, exe, options);
        }

        String value(String key, String fallback) {
            return options.getOrDefault(key, fallback);
        }
    }
}
