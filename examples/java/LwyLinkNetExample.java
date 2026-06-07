import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Collectors;

public final class LwyLinkNetExample {
    private static final Path REPO_ROOT = Path.of("").toAbsolutePath().normalize();
    private static final Path DEFAULT_EXE = REPO_ROOT.resolve("build").resolve("lwylink-net.exe");
    private static final Pattern JSON_STRING_FIELD =
            Pattern.compile("\"([A-Za-z0-9_]+)\"\\s*:\\s*\"((?:\\\\.|[^\"])*)\"");
    private static final Pattern JSON_NUMBER_FIELD =
            Pattern.compile("\"([A-Za-z0-9_]+)\"\\s*:\\s*(-?\\d+)");

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
        exe = resolveExe(exe);
        Process help = new ProcessBuilder(exe.toString(), "--help")
                .redirectErrorStream(true)
                .start();
        String helpOutput;
        try (BufferedReader reader = reader(help)) {
            helpOutput = reader.lines().collect(Collectors.joining(System.lineSeparator()));
        }
        int helpCode = help.waitFor();
        if (helpCode != 0 || !helpOutput.contains("lwylink-net.exe connect")) {
            System.err.println("unexpected help output from lwylink-net.exe");
            System.err.println(helpOutput);
            return helpCode == 0 ? 1 : helpCode;
        }

        List<String> command = List.of(
                exe.toString(),
                "connect",
                "--token", "self-test-token",
                "--user-id", "1",
                "--room-id", "1",
                "--room-public-key", "self-test-room-key",
                "--network-cidr", "172.16.1.0/24",
                "--overlay-addr", "127.0.0.1:1",
                "--server-public-key", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                "--p2p=false"
        );
        Process probe = new ProcessBuilder(command)
                .redirectErrorStream(true)
                .start();
        List<Map<String, String>> events = new ArrayList<>();
        try (BufferedReader reader = reader(probe)) {
            long deadline = System.nanoTime() + Duration.ofSeconds(8).toNanos();
            while (System.nanoTime() < deadline && events.size() < 4) {
                if (!reader.ready()) {
                    Thread.sleep(50);
                    continue;
                }
                String line = reader.readLine();
                if (line == null) {
                    break;
                }
                if (!line.isBlank()) {
                    events.add(parseEvent(line));
                }
                Set<String> types = eventTypes(events);
                if (types.contains("preparing") && types.contains("connecting")) {
                    break;
                }
            }
        } finally {
            terminate(probe);
        }

        Set<String> types = eventTypes(events);
        if (!types.contains("preparing") || !types.contains("connecting")) {
            System.err.println("self-test did not receive expected JSON events");
            System.err.println(events);
            return 1;
        }
        System.out.println("self-test ok: Java launched lwylink-net.exe and parsed JSON status events");
        return 0;
    }

    private static int runConnect(Cli cli) throws Exception {
        Path exe = resolveExe(cli.exe);
        List<String> command = new ArrayList<>();
        command.add(exe.toString());
        command.add("connect");
        add(command, "--api", cli.value("api", env("LWYLINK_API", "http://127.0.0.1:8080")));
        add(command, "--if", cli.value("if-name", env("LWYLINK_IF", "LwyLink")));
        add(command, "--p2p", cli.value("p2p", env("LWYLINK_P2P", "true")));
        addIfPresent(command, "--token", cli.value("token", env("LWYLINK_TOKEN", "")));
        addIfPositive(command, "--room-id", cli.value("room-id", env("LWYLINK_ROOM_ID", "")));
        addIfPositive(command, "--user-id", cli.value("user-id", env("LWYLINK_USER_ID", "")));
        addIfPresent(command, "--room-name", cli.value("room-name", env("LWYLINK_ROOM_NAME", "")));
        addIfPresent(command, "--room-public-key", cli.value("room-public-key", env("LWYLINK_ROOM_PUBLIC_KEY", "")));
        addIfPresent(command, "--network-cidr", cli.value("network-cidr", env("LWYLINK_NETWORK_CIDR", "")));
        addIfPresent(command, "--overlay-addr", cli.value("overlay-addr", env("LWYLINK_OVERLAY_ADDR", "")));
        addIfPresent(command, "--server-public-key", cli.value("server-public-key", env("LWYLINK_SERVER_PUBLIC_KEY", "")));
        return streamProcess(command);
    }

    private static int runCleanup(Cli cli) throws Exception {
        Path exe = resolveExe(cli.exe);
        List<String> command = new ArrayList<>();
        command.add(exe.toString());
        command.add("cleanup");
        add(command, "--if", cli.value("if-name", env("LWYLINK_IF", "LwyLink")));
        addIfPresent(command, "--cidr", cli.value("cidr", env("LWYLINK_NETWORK_CIDR", "")));
        return streamProcess(command);
    }

    private static int streamProcess(List<String> command) throws Exception {
        Process process = new ProcessBuilder(command)
                .redirectErrorStream(true)
                .start();
        Runtime.getRuntime().addShutdownHook(new Thread(() -> terminate(process)));
        try (BufferedReader reader = reader(process)) {
            String line;
            while ((line = reader.readLine()) != null) {
                if (!line.isBlank()) {
                    handleEvent(parseEvent(line));
                }
            }
        }
        return process.waitFor();
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

    private static Map<String, String> parseEvent(String line) {
        Map<String, String> out = new LinkedHashMap<>();
        Matcher stringMatcher = JSON_STRING_FIELD.matcher(line);
        while (stringMatcher.find()) {
            out.put(stringMatcher.group(1), unescapeJsonString(stringMatcher.group(2)));
        }
        Matcher numberMatcher = JSON_NUMBER_FIELD.matcher(line);
        while (numberMatcher.find()) {
            out.putIfAbsent(numberMatcher.group(1), numberMatcher.group(2));
        }
        if (out.isEmpty()) {
            out.put("type", "raw");
            out.put("message", line);
        }
        return out;
    }

    private static String unescapeJsonString(String value) {
        return value
                .replace("\\\"", "\"")
                .replace("\\\\", "\\")
                .replace("\\n", "\n")
                .replace("\\r", "\r")
                .replace("\\t", "\t");
    }

    private static Set<String> eventTypes(List<Map<String, String>> events) {
        return events.stream()
                .map(event -> event.get("type"))
                .collect(Collectors.toSet());
    }

    private static BufferedReader reader(Process process) {
        return new BufferedReader(new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8));
    }

    private static Path resolveExe(Path path) {
        Path resolved = path.isAbsolute() ? path : REPO_ROOT.resolve(path);
        resolved = resolved.normalize();
        if (!Files.exists(resolved)) {
            throw new IllegalArgumentException("lwylink-net.exe not found: " + resolved);
        }
        return resolved;
    }

    private static void terminate(Process process) {
        if (!process.isAlive()) {
            return;
        }
        process.destroy();
        try {
            if (!process.waitFor(3, TimeUnit.SECONDS)) {
                process.destroyForcibly();
            }
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            process.destroyForcibly();
        }
    }

    private static void add(List<String> command, String key, String value) {
        command.add(key);
        command.add(value);
    }

    private static void addIfPresent(List<String> command, String key, String value) {
        if (value != null && !value.isBlank()) {
            add(command, key, value);
        }
    }

    private static void addIfPositive(List<String> command, String key, String value) {
        if (value == null || value.isBlank()) {
            return;
        }
        try {
            if (Long.parseLong(value) > 0) {
                add(command, key, value);
            }
        } catch (NumberFormatException ignored) {
        }
    }

    private static String env(String key, String fallback) {
        String value = System.getenv(key);
        return value == null || value.isBlank() ? fallback : value.trim();
    }

    private static void printUsage() {
        System.out.println("""
                Usage:
                  java -cp examples/java LwyLinkNetExample --self-test
                  java -cp examples/java LwyLinkNetExample connect [options]
                  java -cp examples/java LwyLinkNetExample cleanup [options]

                Common options:
                  --exe <path>              Path to build/lwylink-net.exe
                  --token <token>           Login token; prefer LWYLINK_TOKEN
                  --api <url>               Control API URL
                  --room-id <id>            Room ID
                  --if-name <name>          Wintun adapter name
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
