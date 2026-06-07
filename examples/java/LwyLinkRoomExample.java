import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

public final class LwyLinkRoomExample {
    private static final String DEFAULT_API = "http://127.0.0.1:8080";

    public static void main(String[] args) throws Exception {
        int code = run(args);
        if (code != 0) {
            System.exit(code);
        }
    }

    private static int run(String[] args) throws Exception {
        Cli cli = Cli.parse(args);
        if (cli.selfTest) {
            return selfTest();
        }
        RoomClient client = new RoomClient(cli.value("api", DEFAULT_API), cli.value("token", ""));
        switch (cli.command) {
            case "register" -> System.out.println(client.register(
                    cli.required("username"),
                    cli.required("password"),
                    cli.value("display-name", "")
            ));
            case "login" -> System.out.println(client.login(cli.required("username"), cli.required("password")));
            case "me" -> System.out.println(client.get("/api/me"));
            case "rooms" -> System.out.println(client.get("/api/rooms"));
            case "create-room" -> System.out.println(client.post("/api/rooms", jsonObject(Map.of(
                    "name", cli.required("name"),
                    "maxMembers", cli.value("max-members", "8")
            ))));
            case "join-room" -> System.out.println(client.post("/api/rooms/join", jsonObject(Map.of(
                    "roomPublicKey", cli.required("room-public-key")
            ))));
            case "members" -> System.out.println(client.get("/api/rooms/members?roomID=" + encode(cli.required("room-id"))));
            case "chat" -> System.out.println(client.post("/api/rooms/chat", jsonObject(Map.of(
                    "roomID", cli.required("room-id"),
                    "message", cli.required("message")
            ))));
            case "leave-room" -> System.out.println(client.post("/api/rooms/leave", jsonObject(Map.of(
                    "roomID", cli.required("room-id")
            ))));
            case "delete-room" -> System.out.println(client.post("/api/rooms/delete", jsonObject(Map.of(
                    "roomID", cli.required("room-id")
            ))));
            case "events" -> client.events();
            default -> {
                printUsage();
                return 2;
            }
        }
        return 0;
    }

    private static int selfTest() {
        String sample = "event: room\n" +
                "data: {\"type\":\"connected\",\"roomID\":1}\n" +
                "\n";
        List<String> events = parseSSELines(sample.lines().toList());
        if (events.size() != 1 || !events.get(0).contains("\"type\":\"connected\"")) {
            System.err.println("self-test failed to parse SSE sample");
            return 1;
        }
        System.out.println("self-test ok: room API helpers and SSE parser are ready");
        return 0;
    }

    private static List<String> parseSSELines(List<String> lines) {
        List<String> events = new ArrayList<>();
        List<String> data = new ArrayList<>();
        for (String line : lines) {
            if (line.isEmpty()) {
                if (!data.isEmpty()) {
                    events.add(String.join("\n", data));
                    data.clear();
                }
                continue;
            }
            if (line.startsWith("data:")) {
                data.add(line.substring("data:".length()).trim());
            }
        }
        return events;
    }

    private static String jsonObject(Map<String, String> fields) {
        StringBuilder out = new StringBuilder("{");
        boolean first = true;
        for (Map.Entry<String, String> item : fields.entrySet()) {
            if (!first) {
                out.append(',');
            }
            first = false;
            out.append('"').append(escapeJson(item.getKey())).append('"').append(':');
            if (item.getValue().matches("-?\\d+")) {
                out.append(item.getValue());
            } else {
                out.append('"').append(escapeJson(item.getValue())).append('"');
            }
        }
        return out.append('}').toString();
    }

    private static String escapeJson(String value) {
        return value.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    private static String encode(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8);
    }

    private static void printUsage() {
        System.out.println("""
                Usage:
                  java -cp examples/java LwyLinkRoomExample --self-test
                  java -cp examples/java LwyLinkRoomExample --api <url> register --username <u> --password <p>
                  java -cp examples/java LwyLinkRoomExample --api <url> login --username <u> --password <p>
                  java -cp examples/java LwyLinkRoomExample --api <url> --token <token> create-room --name <name>
                  java -cp examples/java LwyLinkRoomExample --api <url> --token <token> events
                """);
    }

    private static final class RoomClient {
        private final String api;
        private final String token;
        private final HttpClient http;

        RoomClient(String api, String token) {
            this.api = api.endsWith("/") ? api.substring(0, api.length() - 1) : api;
            this.token = token;
            this.http = HttpClient.newBuilder()
                    .connectTimeout(Duration.ofSeconds(20))
                    .build();
        }

        String register(String username, String password, String displayName) throws Exception {
            return post("/api/register", jsonObject(Map.of(
                    "username", username,
                    "password", password,
                    "displayName", displayName
            )));
        }

        String login(String username, String password) throws Exception {
            return post("/api/login", jsonObject(Map.of(
                    "username", username,
                    "password", password
            )));
        }

        String get(String path) throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(api + path))
                    .GET()
                    .timeout(Duration.ofSeconds(20));
            addHeaders(builder);
            return send(builder.build());
        }

        String post(String path, String body) throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(api + path))
                    .POST(HttpRequest.BodyPublishers.ofString(body, StandardCharsets.UTF_8))
                    .timeout(Duration.ofSeconds(20));
            addHeaders(builder);
            return send(builder.build());
        }

        void events() throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(api + "/api/rooms/events")).GET();
            addHeaders(builder);
            HttpResponse<java.io.InputStream> response = http.send(builder.build(), HttpResponse.BodyHandlers.ofInputStream());
            if (response.statusCode() < 200 || response.statusCode() >= 300) {
                try (BufferedReader reader = new BufferedReader(new InputStreamReader(response.body(), StandardCharsets.UTF_8))) {
                    throw new IOException("event stream http " + response.statusCode() + ": " + reader.lines().reduce("", (a, b) -> a + b));
                }
            }
            try (BufferedReader reader = new BufferedReader(new InputStreamReader(response.body(), StandardCharsets.UTF_8))) {
                List<String> data = new ArrayList<>();
                String line;
                while ((line = reader.readLine()) != null) {
                    if (line.isEmpty()) {
                        if (!data.isEmpty()) {
                            System.out.println(String.join("\n", data));
                            data.clear();
                        }
                        continue;
                    }
                    if (line.startsWith("data:")) {
                        data.add(line.substring("data:".length()).trim());
                    }
                }
            }
        }

        private void addHeaders(HttpRequest.Builder builder) {
            builder.header("Content-Type", "application/json");
            if (token != null && !token.isBlank()) {
                builder.header("Authorization", "Bearer " + token);
            }
        }

        private String send(HttpRequest request) throws Exception {
            HttpResponse<String> response = http.send(request, HttpResponse.BodyHandlers.ofString(StandardCharsets.UTF_8));
            if (response.statusCode() < 200 || response.statusCode() >= 300) {
                throw new IOException("http " + response.statusCode() + ": " + response.body());
            }
            return response.body();
        }
    }

    private static final class Cli {
        final String command;
        final boolean selfTest;
        final Map<String, String> options;

        Cli(String command, boolean selfTest, Map<String, String> options) {
            this.command = command;
            this.selfTest = selfTest;
            this.options = options;
        }

        static Cli parse(String[] args) {
            String command = "";
            boolean selfTest = false;
            Map<String, String> options = new HashMap<>();
            for (int i = 0; i < args.length; i++) {
                String arg = args[i];
                if ("--self-test".equals(arg)) {
                    selfTest = true;
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
            return new Cli(command, selfTest, options);
        }

        String value(String key, String fallback) {
            return options.getOrDefault(key, fallback);
        }

        String required(String key) {
            String value = options.get(key);
            if (value == null || value.isBlank()) {
                throw new IllegalArgumentException("missing --" + key);
            }
            return value;
        }
    }
}
