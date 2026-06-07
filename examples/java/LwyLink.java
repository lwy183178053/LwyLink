import java.io.BufferedReader;
import java.io.Closeable;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

public final class LwyLink {
    public static final String DEFAULT_API = "http://127.0.0.1:8080";

    private LwyLink() {
    }

    public static final class RoomClient {
        private final String api;
        private String token;
        private final HttpClient http;

        public RoomClient(String api) {
            this(api, "");
        }

        public RoomClient(String api, String token) {
            this.api = trimRightSlash(api);
            this.token = token == null ? "" : token;
            this.http = HttpClient.newBuilder()
                    .connectTimeout(Duration.ofSeconds(20))
                    .build();
        }

        public String token() {
            return token;
        }

        public String register(String username, String password, String displayName) throws Exception {
            String result = post("/api/register", jsonObject(Map.of(
                    "username", username,
                    "password", password,
                    "displayName", displayName
            )));
            token = jsonField(result, "token");
            return result;
        }

        public String login(String username, String password) throws Exception {
            String result = post("/api/login", jsonObject(Map.of(
                    "username", username,
                    "password", password
            )));
            token = jsonField(result, "token");
            return result;
        }

        public String me() throws Exception {
            return get("/api/me");
        }

        public String announcement() throws Exception {
            return get("/api/announcement");
        }

        public String rooms() throws Exception {
            return get("/api/rooms");
        }

        public String createRoom(String name, int maxMembers) throws Exception {
            return post("/api/rooms", jsonObject(Map.of(
                    "name", name,
                    "maxMembers", String.valueOf(maxMembers)
            )));
        }

        public String joinRoom(String roomPublicKey) throws Exception {
            return post("/api/rooms/join", jsonObject(Map.of("roomPublicKey", roomPublicKey)));
        }

        public String members(long roomID) throws Exception {
            return get("/api/rooms/members?roomID=" + encode(String.valueOf(roomID)));
        }

        public String sendChat(long roomID, String message) throws Exception {
            return post("/api/rooms/chat", jsonObject(Map.of(
                    "roomID", String.valueOf(roomID),
                    "message", message
            )));
        }

        public String leaveRoom(long roomID) throws Exception {
            return post("/api/rooms/leave", jsonObject(Map.of("roomID", String.valueOf(roomID))));
        }

        public String deleteRoom(long roomID) throws Exception {
            return post("/api/rooms/delete", jsonObject(Map.of("roomID", String.valueOf(roomID))));
        }

        public void events(Consumer<String> onEvent) throws Exception {
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
                            onEvent.accept(String.join("\n", data));
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

        public String get(String path) throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(api + path))
                    .GET()
                    .timeout(Duration.ofSeconds(20));
            addHeaders(builder);
            return send(builder.build());
        }

        public String post(String path, String body) throws Exception {
            HttpRequest.Builder builder = HttpRequest.newBuilder(URI.create(api + path))
                    .POST(HttpRequest.BodyPublishers.ofString(body, StandardCharsets.UTF_8))
                    .timeout(Duration.ofSeconds(20));
            addHeaders(builder);
            return send(builder.build());
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

    public static final class ConnectOptions {
        public String api = DEFAULT_API;
        public String token = "";
        public long roomID;
        public String ifName = "LwyLink";
        public boolean p2p = true;
        public long userID;
        public String roomName = "";
        public String roomPublicKey = "";
        public String networkCIDR = "";
        public String overlayAddr = "";
        public String serverPublicKey = "";
    }

    public static final class NetEngine implements Closeable {
        private final Path exe;
        private Process process;
        private Thread readerThread;

        public NetEngine(Path exe) {
            this.exe = resolveExe(exe);
        }

        public Path exe() {
            return exe;
        }

        public synchronized void start(ConnectOptions options, Consumer<Map<String, String>> onEvent) throws IOException {
            if (process != null && process.isAlive()) {
                throw new IllegalStateException("lwylink-net.exe is already running");
            }
            List<String> command = new ArrayList<>();
            command.add(exe.toString());
            command.add("connect");
            add(command, "--api", options.api);
            add(command, "--if", options.ifName);
            command.add("--p2p=" + options.p2p);
            addIfPresent(command, "--token", options.token);
            addIfPositive(command, "--room-id", options.roomID);
            addIfPositive(command, "--user-id", options.userID);
            addIfPresent(command, "--room-name", options.roomName);
            addIfPresent(command, "--room-public-key", options.roomPublicKey);
            addIfPresent(command, "--network-cidr", options.networkCIDR);
            addIfPresent(command, "--overlay-addr", options.overlayAddr);
            addIfPresent(command, "--server-public-key", options.serverPublicKey);
            process = new ProcessBuilder(command).redirectErrorStream(true).start();
            readerThread = new Thread(() -> readEvents(process, onEvent), "lwylink-net-events");
            readerThread.setDaemon(true);
            readerThread.start();
        }

        public int waitFor() throws InterruptedException {
            if (process == null) {
                return 0;
            }
            return process.waitFor();
        }

        public int cleanup(String ifName, String cidr) throws Exception {
            List<String> command = new ArrayList<>();
            command.add(exe.toString());
            command.add("cleanup");
            add(command, "--if", ifName);
            addIfPresent(command, "--cidr", cidr);
            Process cleanup = new ProcessBuilder(command).redirectErrorStream(true).start();
            return cleanup.waitFor();
        }

        @Override
        public void close() {
            terminate(process);
        }

        private static void readEvents(Process process, Consumer<Map<String, String>> onEvent) {
            try (BufferedReader reader = new BufferedReader(new InputStreamReader(process.getInputStream(), StandardCharsets.UTF_8))) {
                String line;
                while ((line = reader.readLine()) != null) {
                    if (!line.isBlank() && onEvent != null) {
                        onEvent.accept(parseEvent(line));
                    }
                }
            } catch (IOException ignored) {
            }
        }
    }

    private static final Pattern JSON_STRING_FIELD =
            Pattern.compile("\"([A-Za-z0-9_]+)\"\\s*:\\s*\"((?:\\\\.|[^\"])*)\"");
    private static final Pattern JSON_NUMBER_FIELD =
            Pattern.compile("\"([A-Za-z0-9_]+)\"\\s*:\\s*(-?\\d+)");

    public static Map<String, String> parseEvent(String line) {
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

    public static String jsonObject(Map<String, String> fields) {
        StringBuilder out = new StringBuilder("{");
        boolean first = true;
        for (Map.Entry<String, String> item : fields.entrySet()) {
            if (!first) {
                out.append(',');
            }
            first = false;
            out.append('"').append(escapeJson(item.getKey())).append('"').append(':');
            if (item.getValue().matches("-?\\d+|true|false")) {
                out.append(item.getValue());
            } else {
                out.append('"').append(escapeJson(item.getValue())).append('"');
            }
        }
        return out.append('}').toString();
    }

    public static String jsonField(String json, String field) {
        Pattern pattern = Pattern.compile("\"" + Pattern.quote(field) + "\"\\s*:\\s*\"((?:\\\\.|[^\"])*)\"");
        Matcher matcher = pattern.matcher(json);
        return matcher.find() ? unescapeJsonString(matcher.group(1)) : "";
    }

    public static String env(String key, String fallback) {
        String value = System.getenv(key);
        return value == null || value.isBlank() ? fallback : value.trim();
    }

    public static boolean envBool(String key, boolean fallback) {
        String value = System.getenv(key);
        if (value == null || value.isBlank()) {
            return fallback;
        }
        return !List.of("0", "false", "no", "off").contains(value.trim().toLowerCase());
    }

    public static Path resolveExe(Path path) {
        Path resolved = path.isAbsolute() ? path : Path.of("").toAbsolutePath().normalize().resolve(path);
        resolved = resolved.normalize();
        if (!Files.exists(resolved)) {
            throw new IllegalArgumentException("lwylink-net.exe not found: " + resolved);
        }
        return resolved;
    }

    public static void terminate(Process process) {
        if (process == null || !process.isAlive()) {
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

    private static String encode(String value) {
        return URLEncoder.encode(value, StandardCharsets.UTF_8);
    }

    private static String trimRightSlash(String value) {
        return value.endsWith("/") ? value.substring(0, value.length() - 1) : value;
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

    private static void addIfPositive(List<String> command, String key, long value) {
        if (value > 0) {
            add(command, key, String.valueOf(value));
        }
    }

    private static String escapeJson(String value) {
        return value.replace("\\", "\\\\").replace("\"", "\\\"");
    }

    private static String unescapeJsonString(String value) {
        return value
                .replace("\\\"", "\"")
                .replace("\\\\", "\\")
                .replace("\\n", "\n")
                .replace("\\r", "\r")
                .replace("\\t", "\t");
    }
}
