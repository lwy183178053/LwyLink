import java.util.HashMap;
import java.util.Map;

public final class LwyLinkRoomExample {
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
        LwyLink.RoomClient client = new LwyLink.RoomClient(cli.value("api", LwyLink.DEFAULT_API), cli.value("token", ""));
        switch (cli.command) {
            case "register" -> System.out.println(client.register(
                    cli.required("email"),
                    cli.required("password"),
                    cli.value("display-name", ""),
                    cli.value("email-code", "")
            ));
            case "send-code" -> System.out.println(client.sendEmailVerification(
                    cli.required("email"),
                    cli.value("purpose", "register")
            ));
            case "reset-password" -> System.out.println(client.resetPassword(
                    cli.required("email"),
                    cli.required("email-code"),
                    cli.required("new-password")
            ));
            case "change-password" -> System.out.println(client.changePassword(
                    cli.required("old-password"),
                    cli.required("new-password")
            ));
            case "login" -> System.out.println(client.login(cli.required("email"), cli.required("password")));
            case "me" -> System.out.println(client.me());
            case "rooms" -> System.out.println(client.rooms());
            case "create-room" -> System.out.println(client.createRoom(
                    cli.required("name"),
                    Integer.parseInt(cli.value("max-members", "8"))
            ));
            case "join-room" -> System.out.println(client.joinRoom(cli.required("room-public-key")));
            case "members" -> System.out.println(client.members(Long.parseLong(cli.required("room-id"))));
            case "chat" -> System.out.println(client.sendChat(Long.parseLong(cli.required("room-id")), cli.required("message")));
            case "leave-room" -> System.out.println(client.leaveRoom(Long.parseLong(cli.required("room-id"))));
            case "delete-room" -> System.out.println(client.deleteRoom(Long.parseLong(cli.required("room-id"))));
            case "events" -> client.events(System.out::println);
            default -> {
                printUsage();
                return 2;
            }
        }
        return 0;
    }

    private static int selfTest() {
        Map<String, String> event = LwyLink.parseEvent("{\"type\":\"connected\",\"roomID\":1}");
        if (!"connected".equals(event.get("type")) || !"1".equals(event.get("roomID"))) {
            System.err.println("self-test failed to parse event sample");
            return 1;
        }
        System.out.println("self-test ok: room API library is ready");
        return 0;
    }

    private static void printUsage() {
        System.out.println("""
                Usage:
                  javac examples/java/LwyLink.java examples/java/LwyLinkRoomExample.java
                  java -cp examples/java LwyLinkRoomExample --self-test
                  java -cp examples/java LwyLinkRoomExample send-code --email <email> --purpose register
                  java -cp examples/java LwyLinkRoomExample register --email <email> --password <p> --email-code <code>
                  java -cp examples/java LwyLinkRoomExample reset-password --email <email> --email-code <code> --new-password <p>
                  java -cp examples/java LwyLinkRoomExample login --email <email> --password <p>
                  java -cp examples/java LwyLinkRoomExample --token <token> create-room --name <name>
                  java -cp examples/java LwyLinkRoomExample --token <token> change-password --old-password <p> --new-password <p>
                  java -cp examples/java LwyLinkRoomExample --token <token> events

                Default API is https://www.lwylink.xyz. Pass --api <url> when targeting a local server.
                """);
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
