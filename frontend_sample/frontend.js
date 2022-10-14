
var gTwoTierComments = false
var gCommonBlockStyle = "border-style: solid; border-color: rgb(64, 64, 64); border-width:thin";
url = window.location.origin + window.location.pathname;

$(document).ready(function () {
  var logged = IsLoggedIn();
  console.log("loggedin: ", logged);

  console.log(url);
  //console.log(window.location.hash);

  backend_url = "http://localhost:3000";
  get_posts_url = backend_url + `/api/v1/posts?url=${url}&sort=default`;
  $.get(get_posts_url, function(data) {
    console.log(data)

    let root_level_comments = []
    let reply_level_comments = []

    let contents_html = ""
    if (data.length > 0)
    {
      for (let comment of data)
      {
        let depth = comment["Depth"];
        if (depth <= 0)
          root_level_comments.push(comment)
        else
          reply_level_comments.push(comment)
      }

      for (let comment of root_level_comments)
      {
        let username = comment["User"];
        let contents = comment["HtmlText"];
        let timestamp = comment["Timestamp"];
        let date = new Date (timestamp);
        let comment_id = comment["ID"];

        if (username == "")
          username = "Anonymous";

        let comment_block_style = `margin:10px 0px 10px 0px;`;
        comment_block_style += gCommonBlockStyle;

        let comment_contents_html = 
          `
          <div id="commentblock_${comment_id}" style="${comment_block_style}">
            <div style="margin-left:3px">
              <div style="color:blue;font-style:bold;font-size:120%;margin-bottom:1px">
               <a href="#commentblock_${comment_id}">${username}</a>
              </div>
              <div style="color:blue;font-style:italic;font-size:70%;margin-bottom:5px">${date.toLocaleString()}</div>
              <div style="margin-bottom:5px">${contents}</div>
              <a id="reply_${comment_id}" href="#" onclick="addReplyBox(this, event, event.target.id, 0, true);" style="font-size:80%">Reply</a>
            </div>
          </div>
          `;

        contents_html += comment_contents_html;
      }
      document.getElementById("master_comments_div").innerHTML = "<div>" + contents_html + "</div>";

      //console.log(reply_level_comments)
      for (let comment of reply_level_comments)
      {
        let username = comment["User"];
        let contents = comment["HtmlText"];
        let parentid = comment["ParentID"];
        let timestamp = comment["Timestamp"];
        let date = new Date (timestamp);
        let comment_id = comment["ID"];
        let depth = comment["Depth"];

        if (username == "")
          username = "Anonymous";
        let addendum = ""
        if (depth == 1)
          addendum = " to root comment";

        let margin_left = "20px";
        if (gTwoTierComments == true)
        {
          margin_left = depth == 1 ? "10px" : "0px";
        }
        let comment_block_style = `margin:10px 0px 10px ${margin_left};`;
        comment_block_style += gCommonBlockStyle;

        //let comment_block_style = "margin:10px 0px 10px 20px"; //layer 2

        let comment_contents_html = 
          `
          <div id="commentblock_${comment_id}" style="${comment_block_style}">
            <div style="margin-left:3px">
              <div style="color:blue;font-style:bold;font-size:120%;margin-bottom:1px">
                <a href="#commentblock_${comment_id}">${username}</a>
              </div>
              <div style="color:blue;font-style:italic;font-size:70%;margin-bottom:5px">${date.toLocaleString()} ${addendum}</div>
              <div style="margin-bottom:5px">${contents}</div>
              <a id="reply_${comment_id}" href="#" onclick="addReplyBox(this, event, event.target.id, 0, true);" style="font-size:80%">Reply</a>
            </div>
          </div>
          `;
          
        //document.getElementById(`commentblock_${parentid}`).insertAdjacentHTML("afterend", comment_contents_html)
        console.log(parentid);
        var lastchild = document.getElementById(`commentblock_${parentid}`).lastElementChild
        lastchild.insertAdjacentHTML("afterend", comment_contents_html)
      }

      document.getElementById("master_comments_div").innerHTML += generateReplyFormHtml("", 0, false);
    }
    else
    {
      document.getElementById("master_comments_div").innerHTML = "No comments";
      document.getElementById("master_comments_div").innerHTML += generateReplyFormHtml("", 0, false);
    }
  }).fail(function() {
    document.getElementById("master_comments_div").innerHTML = "Error fetching comments";
  });
});

async function submitComment(event, formid) {
  event.preventDefault();

  let data = formid.split("_");
  let id = data[1];
  let form_error_msg_id = `form_error_msg_${id}`;

  // if not auth
  let username = "";
  let email = "";
  if (IsLoggedIn())
    username = localStorage["username"];
  else
  {
    username = document.getElementById(`username_input_${id}`).value;
    email = document.getElementById(`email_input_${id}`).value;
  }
  if (username == "")
      return;
  
  //console.log(username);
  
  if (!IsLoggedIn())
  {
    var jsonbody = {
      "username": username,
      "url": url,
      "email" : email
    }

    auth_url = backend_url + "/auth/anonymous";
    const response = await fetch(auth_url, {
      method: "POST",
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify(jsonbody),
    });
    console.log(response);

    if (response["status"] != 200)
    {
      if (response.body == null)
        document.getElementById(form_error_msg_id).textContent = 
          "Error: " + response["status"];
      else
        document.getElementById(form_error_msg_id).textContent = 
          await response.text();
      
      return;
    }
    else
    {
      // 
      localStorage["username"] = username;
      localStorage["expires"] = Date.now() + 14 * 24 * 3600 * 1000; // 14 days
      //localStorage["expires"] = Date.now() + 20 * 1000; //20s, testing
    }
  }

  var text = document.getElementById(`comment_textarea_${id}`).value;
  console.log(text);
  console.log(username);

  jsonbody = {
    "text": text,
    "url": url,
    "username": username,
    "parent_id": data[1],
  }
  const comment_response = await fetch(backend_url + "/api/v1/comment", {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(jsonbody),
    credentials: 'include'
  });
  console.log(comment_response);

  if (comment_response["status"] != 200)
  {
    if (comment_response.body == null)
      document.getElementById(form_error_msg_id).textContent = 
        "Error: " + comment_response["status"];
    else
      document.getElementById(form_error_msg_id).textContent = 
        await comment_response.text();
    
    return;
  }

  location.reload();
}

async function addReplyBox(element, event, reply_id, margin_offset_x, show_cancel_btn) {
  event.preventDefault();
  console.log("reply: ", reply_id);
  let data = reply_id.split("_");
  let id = data[1];
  let form_id = "form_" + id;
  if (document.getElementById(form_id) != null)
    return;
  console.log(document.getElementById(form_id));
  
  //let comment_block_id = "commentblock_" + id;
  element.insertAdjacentHTML("afterend",
    generateReplyFormHtml(id, margin_offset_x, show_cancel_btn));
}

function generateReplyFormHtml(id, margin_offset_x, show_cancel_btn) {
  let form_id = `form_${id}`;
  let textarea_id = `comment_textarea_${id}`;
  let username_input_id = `username_input_${id}`;
  let email_input_id = `email_input_${id}`;
  let form_error_msg_id = `form_error_msg_${id}`;
  let cancel_btn_html = show_cancel_btn == true ? 
    `<button type="button" onclick="hideReplyBox(event, this)">Cancel Reply</button>` : "";

  let username_and_email = IsLoggedIn() ? 
    `<span>Posting as: ${localStorage["username"]}</span>` :
    `
    <span>Name:
      <input id="${username_input_id}" style="margin-left:10px" type="text" required/>
    </span>
    <span style="margin-left:20px">Email (optional):
      <input id="${email_input_id}" type="email" style="margin-left:10px;margin-bottom:10px" />
    </span>
    <br>
    <a href="http://localhost:3000/auth/google?url_initiate_auth=${url}" class="btn btn-danger"><span class="fa fa-google"></span>Google Signin</a>`;

  let formhtml = 
    `<form style="width:40%; padding:5px 5px 5px 5px; border-style: solid; border-color: rgb(8, 128, 128);
      margin-left: ${margin_offset_x}px; margin-bottom: 5px"
      onsubmit="submitComment(event, event.target.id)" id="${form_id}">
      <div>Leave a reply (<a href="https://www.markdownguide.org/basic-syntax/">Markdown</a> supported, no images):</div>
      <textarea id="${textarea_id}" style="width:98%;" rows="10" cols="50"></textarea> 
      <br>
      </div>
      <div style="margin-top: 15px;">
        ${username_and_email}
        <div id="site_question" style="margin-top: 5px;">What is the name of this website?
          <input type="text" style="margin-left:10px" required/>
        </div>
      </div>
      
      <input type="submit" value="Submit" style="margin-top: 5px;"></input>
      ${cancel_btn_html}
      <span id="${form_error_msg_id}" style="color: red;"></span>
    </form>`;
  return formhtml;
}

async function hideReplyBox(event, element) {
  event.preventDefault();
  let form_element = element.parentElement;
  form_element.remove();
}

function IsLoggedIn() {
  var allcookies = document.cookie;

  console.log("All Cookies : " + allcookies);

  let usrname = getCookie("Username")
  if (usrname != null)
  {
    console.log(usrname);
    localStorage["username"] = usrname;
    localStorage["expires"] = Date.now() + 14 * 24 * 3600 * 1000; // 14 days
    return true;
  }

  if (localStorage["username"] != null && Date.now() < localStorage["expires"])
    return true;
  localStorage.removeItem("username");
  localStorage.removeItem("expires");
  return false;
}

var getCookie = function (name) {
	var value = "; " + document.cookie;
	var parts = value.split("; " + name + "=");
	if (parts.length == 2) 
  {
    var ret = parts.pop().split(";").shift();
    if (ret[0] == '"' && ret[ret.length - 1] == '"')
      ret = ret.replace(/['"]+/g, '');

    return ret
  }
  return null
};