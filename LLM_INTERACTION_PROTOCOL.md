# The LLM Interaction Protocol

Type: Tech Spec

Status: Active

Author: Mutian Guo

Products: SciEdu (https://www.notion.so/SciEdu-2f27dadd8040801d8d77d2c4829c7015?pvs=21)

Created time: March 12, 2026 8:48 PM

Last edited time: April 1, 2026 3:20 PM

Last Updated: Sprint 5

## An LLM User Journey

1. Alice asks the LLM a question.
    
2. The LLM gives Alice an answer.
    
3. Alice asks the LLM a second question.
    
4. While the LLM is halfway through answering, Alice accidentally disconnects from the internet. Upon reconnecting, she sees the LLM is still continuously generating the answer.
    
5. Alice is not satisfied with the LLM's second answer, so she regenerates a response.
    
6. Alice is still unsatisfied with the second regenerated response, so she edits the prompt of her second question.
    
7. The LLM's answer to the new prompt is even worse. Alice gets annoyed and decides to switch back to the thread of the first prompt to continue asking follow-up questions.
    

From the journey above, you might realize that ~~users are very annoying creatures~~ writing a good LLM App is not that simple. Therefore, the core of this technical specification focuses on defining the behaviors of the LLM-related components in SciEdu.

## The Two LLM Data Models

An interaction with the LLM involves two types of objects:

- `Message`: The specific interaction, including the role (whether sent by the user or the LLM) and the content (what was said).
    
- `Conversation`: An array containing multiple `Message`s, where each `Message` records its own `previousMessageID`.
    

## Conversation Flows

### When a user starts a new conversation

```
sequenceDiagram
    participant FE as Frontend 
    participant BE as Backend
    participant LLM as LLM Module
    
    FE->>BE: Send chat initialization message
    BE-->>FE: Return ConversationID
    BE->>LLM: Initialize message stream with conversation content
    LLM->>BE: SSE stream
    FE->>BE: Request response stream 
    BE->>FE: Return response stream
    LLM->>BE: Streaming complete, close SSE
    BE->>FE: Streaming complete, close SSE
    FE->>BE: Request final message
    BE->>FE: Respond final message
```

### User loads a Conversation without streaming Messages

```
sequenceDiagram
    participant FE as Frontend
    participant BE as Backend

    FE->>BE: GET /chat/:chatID/messages
    BE-->>FE: Messages array (all status: "completed")

    Note over FE: Check message statuses<br/>All status = "completed"

    Note over FE: Render conversation as-is<br/>No SSE connection needed
```

### User loads a Conversation with a partially streaming Message

```
sequenceDiagram
    participant FE as Frontend
    participant BE as Backend
    participant LLM as LLM Module

    Note over BE,LLM: SSE stream already in progress<br/>(Backend is buffering partial content)

    LLM->>BE: SSE delta events (ongoing)

    FE->>BE: GET /chat/:chatID/messages
    BE-->>FE: Messages array<br/>(one message has status: "streaming",<br/>content = buffered partial content)

    Note over FE: Detect message with status "streaming"<br/>Render partial content immediately

    FE->>BE: GET /chat/stream/:messageID (SSE)

    loop LLM still streaming
        LLM->>BE: SSE delta
        BE->>FE: event: delta data: {"content":"..."}
    end

    LLM->>BE: Stream complete, close SSE
    BE->>FE: event: done (close SSE)

    Note over BE: Update message<br/>status → "completed"

    FE->>BE: GET /chat/:chatID/messages
    BE-->>FE: Final message (status: "completed")
```

### User continues a discussion in an existing conversation

```
sequenceDiagram
    participant FE as Frontend
    participant BE as Backend
    participant LLM as LLM Module

    Note over FE: User types message in<br/>existing conversation

    FE->>BE: POST /chat/:chatID/messages<br/>{content: "...", previousID: "uuid"}

    BE-->>FE: {message: {id, status: "created", ...},<br/>replyMessageID: "uuid"}

    Note over FE: Optimistic update:<br/>Render user message + assistant placeholder

    BE->>LLM: Initialize stream with conversation context
    LLM->>BE: SSE stream begins

    Note over BE: Update reply message<br/>status → "streaming"

    FE->>BE: GET /chat/stream/:replyMessageID (SSE)

    loop LLM streaming
        LLM->>BE: SSE delta
        BE->>FE: event: delta data: {"content":"..."}
    end

    Note over FE: Append deltas to<br/>assistant placeholder

    LLM->>BE: Stream complete, close SSE
    BE->>FE: event: done (close SSE)

    Note over BE: Update reply message<br/>status → "completed"

    FE->>BE: GET /chat/:chatID/messages
    BE-->>FE: Final messages (all status: "completed")

    Note over FE: Reconcile optimistic state<br/>with server response
```

### Backend Behavior upon LLM Module Error

```
sequenceDiagram
    participant FE as Frontend
    participant BE as Backend
    participant LLM as LLM Module

    LLM->>BE: SSE error event
    BE->>FE: event: error (close SSE)

    Note over BE: Update message<br/>status → "failed"

    FE->>BE: GET /chat/stream/:messageID (SSE)
    BE-->>FE: 404 (stream not found)

    FE->>BE: GET /chat/:chatID/messages
    BE-->>FE: 502 → message status: "failed"

    Note over FE: Render error state<br/>(e.g. retry button)
```

## API Behaviors

### When a new SSE connection is established

- If there is currently no active SSE, return a 4XX error code as defined by the API Spec.
    
- If there is an SSE currently streaming halfway through:
    
    - First, yield a chunk containing the data that has already been fully streamed.
        
    - Then, as the backend receives new chunks, continuously stream the new chunks to the frontend.
        
    - For example:
        
        ```
        sequenceDiagram
            participant FE as Frontend
            participant BE as Backend
            participant LLM as LLM Module
        
                LLM->>BE: Starts SSE stream
                LLM-->>BE: Hello
                LLM-->>BE: world
                LLM-->>BE: sample
                LLM-->>BE: stream
        
            FE->>BE: GET /chat/stream/:streamId
            BE-->>FE: chunk "data": "Hello world sample stream"
        
                LLM-->>BE: looks
                BE-->>FE: looks
        ```